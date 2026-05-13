package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jsw-teams/myfiles/internal/account"
	"github.com/jsw-teams/myfiles/internal/audit"
	"github.com/jsw-teams/myfiles/internal/config"
	myfiles "github.com/jsw-teams/myfiles/internal/files"
	"github.com/jsw-teams/myfiles/internal/ids"
	"github.com/jsw-teams/myfiles/internal/storage"
)

type App struct {
	mu         sync.RWMutex
	cfg        config.Config
	configPath string
	db         *sql.DB
	account    *account.Client
	storage    storage.Uploader
}

func New(cfg config.Config, database *sql.DB, accountClient *account.Client, uploader storage.Uploader) *App {
	return &App{cfg: cfg, configPath: cfg.SourcePath, db: database, account: accountClient, storage: uploader}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.dispatch)
	return a.securityHeaders(mux)
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/admin") || strings.HasPrefix(p, "/dashboard") || strings.HasPrefix(p, "/setup") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		if isProbePath(p) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isProbePath(p string) bool {
	probes := []string{"/.env", "/bin/.env", "/logs/.env", "/scripts/.env", "/asset-manifest.json", "/build-manifest.json", "/webpack-stats.json", "/stats.json"}
	for _, v := range probes {
		if p == v {
			return true
		}
	}
	prefixes := []string{"/.git/", "/.next/", "/_next/", "/_nuxt/", "/.astro/", "/.vite/"}
	for _, v := range prefixes {
		if strings.HasPrefix(p, v) {
			return true
		}
	}
	return false
}

func (a *App) dispatch(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/healthz":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "myfiles"})
	case p == "/api/bootstrap":
		a.handleBootstrap(w, r)
	case p == "/auth/account/start":
		a.handleAuthStart(w, r)
	case p == "/auth/account/callback":
		a.handleAuthCallback(w, r)
	case p == "/api/account/me":
		a.handleAccountMe(w, r)
	case p == "/api/auth/logout":
		a.handleLogout(w, r)
	case p == "/api/upload":
		a.handleUpload(w, r)
	case p == "/api/files":
		a.handleFiles(w, r)
	case strings.HasPrefix(p, "/api/files/"):
		a.handleFileAPI(w, r)
	case strings.HasPrefix(p, "/api/uploads/"):
		a.handleUploadBatch(w, r)
	case p == "/api/admin/files":
		a.handleAdminFiles(w, r)
	case strings.HasPrefix(p, "/api/admin/files/"):
		a.handleAdminFileAPI(w, r)
	case strings.HasPrefix(p, "/admin/open/"):
		a.handleAdminOpenFile(w, r)
	case p == "/api/admin/audit":
		a.handleAdminAudit(w, r)
	case p == "/api/admin/settings":
		a.handleAdminSettings(w, r)
	case p == "/api/admin/storage/test":
		a.handleAdminStorageTest(w, r)
	case strings.HasPrefix(p, "/file/"), strings.HasPrefix(p, "/f/"):
		a.handlePublicFile(w, r)
	default:
		a.serveFrontend(w, r)
	}
}

func (a *App) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	policy := a.effectiveUploadPolicy()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"brand": map[string]any{"name": "myfiles", "domain": "files.js.gripe"},
		"upload": map[string]any{
			"maxBytes":         policy.MaxBytes,
			"allowedMimeTypes": policy.AllowedMIMETypes,
			"allowAnonymous":   policy.AllowAnonymous,
		},
		"account": map[string]any{"loginPath": "/login", "startPath": "/auth/account/start?popup=1"},
	})
}

func (a *App) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	state := ids.New("st")
	http.SetCookie(w, &http.Cookie{
		Name: "myfiles_oauth_state", Value: state, Path: "/auth/account", MaxAge: 600,
		HttpOnly: true, Secure: a.cfg.Security.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	u, _ := url.Parse(a.cfg.Account.LoginURL)
	q := u.Query()
	q.Set("client_id", a.cfg.Account.ClientID)
	q.Set("redirect_uri", a.cfg.Account.RedirectURI)
	q.Set("scope", strings.Join(a.cfg.Account.Scopes, " "))
	q.Set("state", state)
	q.Set("prompt", "consent")
	if r.URL.Query().Get("popup") == "1" {
		q.Set("popup", "1")
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (a *App) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		a.popupHTML(w, false, "/login?error=auth_failed", "统一账户授权未完成")
		return
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie("myfiles_oauth_state")
	if err != nil || c.Value == "" || state == "" || c.Value != state {
		a.popupHTML(w, false, "/login?error=state", "登录状态校验失败，请重新打开登录窗口")
		return
	}
	accountSession := r.URL.Query().Get("account_session")
	if accountSession == "" {
		a.popupHTML(w, false, "/login?error=no_session", "账户中心未返回有效登录状态")
		return
	}
	user, err := a.account.Me(r.Context(), accountSession)
	if err != nil {
		code, msg := "account_error", "统一账户校验失败，请稍后重试"
		if e, ok := err.(*account.APIError); ok {
			code = e.Code
			msg = e.Message
		}
		a.popupHTML(w, false, "/login?error="+url.QueryEscape(code), msg)
		return
	}
	if err := a.createSession(w, user); err != nil {
		a.popupHTML(w, false, "/login?error=session", "文件服务会话创建失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "myfiles_oauth_state", Value: "", Path: "/auth/account", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Security.CookieSecure, SameSite: http.SameSiteLaxMode})
	audit.Write(a.db, r, audit.Actor{AccountUserID: user.ID, Role: user.Role}, "auth.login", "account_user", user.ID, map[string]any{"client": "myfiles"})
	a.popupHTML(w, true, "/dashboard", "登录成功")
}

func (a *App) popupHTML(w http.ResponseWriter, ok bool, redirectTo, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	status := "error"
	if ok {
		status = "ok"
	}
	encMsg, _ := json.Marshal(message)
	encTo, _ := json.Marshal(redirectTo)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>myfiles login</title><body>
<script>
const payload={source:"myfiles-auth",status:%q,message:%s,redirectTo:%s};
try{ if(window.opener){ window.opener.postMessage(payload, window.location.origin); window.close(); } else { location.href=payload.redirectTo; } }
catch(e){ location.href=payload.redirectTo; }
</script>
<p>%s</p></body>`, status, encMsg, encTo, message)
}

func (a *App) handleAccountMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"user":               s.User,
		"myfilesPermissions": s.Permissions,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	s, _ := a.readSession(r)
	a.clearSession(w, r)
	if s != nil {
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "auth.logout", "account_user", s.User.ID, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	policy := a.effectiveUploadPolicy()
	s, _ := a.readSession(r)
	if s == nil && !policy.AllowAnonymous {
		writeError(w, 401, "unauthorized", "请先登录后上传", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, policy.MaxBytes+1<<20)
	if err := r.ParseMultipartForm(policy.MaxBytes); err != nil {
		writeError(w, 413, "upload_too_large", "文件超过当前允许的上传限制", nil)
		return
	}
	var headers []*multipart.FileHeader
	if r.MultipartForm != nil {
		headers = append(headers, r.MultipartForm.File["file"]...)
		headers = append(headers, r.MultipartForm.File["files"]...)
	}
	if len(headers) == 0 {
		writeError(w, 400, "file_required", "没有收到文件", nil)
		return
	}

	owner := ""
	role := ""
	if s != nil {
		owner = s.User.ID
		role = s.User.Role
	}
	batch, err := myfiles.CreateBatch(a.db, owner)
	if err != nil {
		writeError(w, 500, "db_error", "创建上传批次失败", nil)
		return
	}

	type item struct {
		OK    bool   `json:"ok"`
		File  any    `json:"file,omitempty"`
		Error string `json:"error,omitempty"`
		Code  string `json:"code,omitempty"`
		Name  string `json:"name,omitempty"`
	}
	total, success, failed := len(headers), 0, 0
	var items []item
	for _, fh := range headers {
		file, code, err := a.processOneUpload(r, batch.ID, owner, fh, policy)
		if err != nil {
			failed++
			items = append(items, item{OK: false, Name: fh.Filename, Code: code, Error: err.Error()})
			continue
		}
		success++
		items = append(items, item{OK: true, File: file, Name: fh.Filename})
	}
	status := "completed"
	if failed > 0 && success == 0 {
		status = "failed"
	} else if failed > 0 {
		status = "partial"
	}
	_ = myfiles.UpdateBatchCounts(a.db, batch.ID, total, success, failed, status)
	audit.Write(a.db, r, audit.Actor{AccountUserID: owner, Role: role}, "upload.create", "upload_batch", batch.ID, map[string]any{"total": total, "success": success, "failed": failed})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "batchId": batch.ID, "status": status, "items": items, "resultPath": "/uploads/" + batch.ID})
}

func (a *App) processOneUpload(r *http.Request, batchID, owner string, fh *multipart.FileHeader, policy uploadPolicy) (myfiles.File, string, error) {
	src, err := fh.Open()
	if err != nil {
		return myfiles.File{}, "file_open_failed", fmt.Errorf("无法读取文件")
	}
	defer src.Close()

	fileID := ids.New("fil")
	tmpPath := filepath.Join(a.cfg.App.TempDir, fileID+".upload")
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return myfiles.File{}, "temp_create_failed", fmt.Errorf("创建临时文件失败")
	}
	defer os.Remove(tmpPath)

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, policy.MaxBytes+1))
	if closeErr := tmp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return myfiles.File{}, "temp_write_failed", fmt.Errorf("写入临时文件失败")
	}
	if n > policy.MaxBytes {
		return myfiles.File{}, "upload_too_large", fmt.Errorf("文件超过当前允许的上传限制")
	}

	mimeType, err := detectMIME(tmpPath)
	if err != nil {
		return myfiles.File{}, "mime_detect_failed", fmt.Errorf("文件类型检测失败")
	}
	if !mimeAllowed(mimeType, policy.AllowedMIMETypes) {
		return myfiles.File{}, "mime_not_allowed", fmt.Errorf("当前文件类型不允许上传")
	}

	var width, height *int
	if strings.HasPrefix(mimeType, "image/") {
		if w, h, ok := imageSize(tmpPath); ok {
			width = &w
			height = &h
		}
	}
	name := filepath.Base(fh.Filename)
	shaHex := hex.EncodeToString(h.Sum(nil))

	up, err := a.currentStorage().Upload(r.Context(), storage.UploadInput{TempPath: tmpPath, FileID: fileID, Filename: name, MIME: mimeType, SHA256: shaHex, Size: n})
	if err != nil {
		return myfiles.File{}, "storage_upload_failed", fmt.Errorf("存储通道上传失败：%v", err)
	}

	publicURL := a.cfg.App.BaseURL + publicFilePath(fileID, fh.Filename)
	if up.PublicURL != "" {
		publicURL = up.PublicURL
	}
	f, err := myfiles.CreateFile(a.db, myfiles.CreateFileInput{
		ID: fileID, BatchID: batchID, OwnerUserID: owner, OriginalName: name, StoredName: up.FileID,
		MIME: mimeType, Size: n, SHA256: shaHex, ImageWidth: width, ImageHeight: height,
		StorageProvider: up.Provider, StorageFileID: up.FileID, StorageURL: up.URL, PublicURL: publicURL,
		IsPublic: policy.DefaultPublic, RequireConfirm: policy.DefaultRequireConfirm,
		RegionPolicy: policy.DefaultRegionPolicy, HotlinkPolicy: policy.DefaultHotlinkPolicy,
	})
	if err != nil {
		return myfiles.File{}, "db_error", fmt.Errorf("保存文件记录失败")
	}
	_, _ = a.db.Exec(`INSERT INTO file_events (id, file_id, batch_id, owner_user_id, event_type, detail_json, created_at)
		VALUES (?, ?, ?, ?, 'uploaded', '{}', ?)`, ids.New("evt"), f.ID, batchID, nullEmpty(owner), time.Now().UTC().Format(time.RFC3339))
	return f, "", nil
}

func detectMIME(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	if n == 0 {
		return "application/octet-stream", nil
	}
	return http.DetectContentType(buf[:n]), nil
}

func imageSize(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

func mimeAllowed(mimeType string, allowed []string) bool {
	for _, rule := range allowed {
		rule = strings.TrimSpace(strings.ToLower(rule))
		if rule == "*" || rule == "*/*" || rule == strings.ToLower(mimeType) {
			return true
		}
		if strings.HasSuffix(rule, "/*") && strings.HasPrefix(strings.ToLower(mimeType), strings.TrimSuffix(rule, "*")) {
			return true
		}
	}
	return false
}

func (a *App) handleFiles(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	list, err := myfiles.ListFiles(a.db, myfiles.ListOptions{OwnerUserID: s.User.ID, Query: r.URL.Query().Get("q"), Limit: limit(r, 100)})
	if err != nil {
		writeError(w, 500, "db_error", "读取文件列表失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "files": list})
}

func (a *App) handleFileAPI(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/files/")
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil {
		writeError(w, 404, "not_found", "文件不存在", nil)
		return
	}
	if f.OwnerUserID != s.User.ID && !s.Permissions.AllFilesRead {
		writeError(w, 403, "forbidden", "无权访问该文件", nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"ok": true, "file": f})
	case http.MethodDelete:
		if f.OwnerUserID != s.User.ID && !s.Permissions.AllFilesWrite {
			writeError(w, 403, "forbidden", "无权删除该文件", nil)
			return
		}
		if err := myfiles.SoftDelete(a.db, id); err != nil {
			writeError(w, 500, "db_error", "删除文件失败", nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "file.delete", "file", id, map[string]any{"owner": f.OwnerUserID})
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
	}
}

func (a *App) handleUploadBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/uploads/")
	b, list, err := myfiles.GetBatch(a.db, id)
	if err != nil {
		writeError(w, 404, "not_found", "上传批次不存在", nil)
		return
	}
	s, _ := a.readSession(r)
	if b.OwnerUserID != "" {
		if s == nil {
			writeError(w, 401, "unauthorized", "请先登录查看该上传批次", nil)
			return
		}
		if s.User.ID != b.OwnerUserID && !s.Permissions.AllFilesRead {
			writeError(w, 403, "forbidden", "无权查看该上传批次", nil)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "batch": b, "files": list})
}

func (a *App) handleAdminFiles(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.AllFilesRead })
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	list, err := myfiles.ListFiles(a.db, myfiles.ListOptions{All: true, Query: r.URL.Query().Get("q"), OwnerFilter: r.URL.Query().Get("owner"), Limit: limit(r, 200)})
	if err != nil {
		writeError(w, 500, "db_error", "读取全部文件失败", nil)
		return
	}
	_ = s
	writeJSON(w, 200, map[string]any{"ok": true, "files": list})
}

func (a *App) handleAdminFileAPI(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.AllFilesRead })
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/files/")
	switch r.Method {
	case http.MethodPatch:
		if !s.Permissions.AllFilesWrite {
			writeError(w, 403, "forbidden", "无权修改文件策略", nil)
			return
		}
		var body struct {
			IsPublic       *bool  `json:"isPublic"`
			RequireConfirm *bool  `json:"requireConfirm"`
			RegionPolicy   string `json:"regionPolicy"`
			HotlinkPolicy  string `json:"hotlinkPolicy"`
			Status         string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "bad_json", "请求体格式错误", nil)
			return
		}
		if err := myfiles.PatchAdmin(a.db, id, body.IsPublic, body.RequireConfirm, body.RegionPolicy, body.HotlinkPolicy, body.Status); err != nil {
			writeError(w, 500, "db_error", "更新文件策略失败", nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "admin.file.patch", "file", id, body)
		writeJSON(w, 200, map[string]any{"ok": true})
	case http.MethodDelete:
		if !s.Permissions.AllFilesWrite {
			writeError(w, 403, "forbidden", "无权代管删除文件", nil)
			return
		}
		f, _ := myfiles.GetFile(a.db, id, true)
		if err := myfiles.SoftDelete(a.db, id); err != nil {
			writeError(w, 500, "db_error", "删除文件失败", nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "admin.file.delete", "file", id, map[string]any{"owner": f.OwnerUserID})
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
	}
}

func (a *App) handleAdminOpenFile(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.AllFilesRead })
	if !ok {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/open/")
	f, err := myfiles.GetFile(a.db, publicFileID(id), false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if f.StorageProvider == "local" && f.StorageURL != "" {
		w.Header().Set("Content-Type", f.MIME)
		w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(f.OriginalName))
		w.Header().Set("Cache-Control", "private, max-age=0, no-store")
		http.ServeFile(w, r, f.StorageURL)
		return
	}
	if f.StorageProvider == "tgbots" && f.StorageFileID != "" {
		f.IsPublic = false
		a.streamTGBotsFile(w, r, f)
		return
	}
	if f.StorageURL != "" {
		http.Redirect(w, r, f.StorageURL, http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (a *App) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	_, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.AuditRead })
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	rows, err := a.db.Query(`SELECT id, COALESCE(actor_account_user_id,''), COALESCE(actor_role,''), action, target_type, COALESCE(target_id,''), detail_json, COALESCE(ip,''), COALESCE(user_agent,''), created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT ?`, limit(r, 200))
	if err != nil {
		writeError(w, 500, "db_error", "读取审计日志失败", nil)
		return
	}
	defer rows.Close()
	var logs []map[string]any
	for rows.Next() {
		var id, actor, role, action, targetType, targetID, detail, ip, ua, created string
		_ = rows.Scan(&id, &actor, &role, &action, &targetType, &targetID, &detail, &ip, &ua, &created)
		logs = append(logs, map[string]any{"id": id, "actorAccountUserId": actor, "actorRole": role, "action": action, "targetType": targetType, "targetId": targetID, "detail": json.RawMessage(detail), "ip": ip, "userAgent": ua, "createdAt": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "logs": logs})
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.SettingsRead })
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings := configSettings(a.snapshotConfig())
		writeJSON(w, 200, map[string]any{"ok": true, "settings": settings})
	case http.MethodPatch:
		if !s.Permissions.SettingsWrite {
			writeError(w, 403, "forbidden", "无权修改站点设置", nil)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "bad_json", "请求体格式错误", nil)
			return
		}
		if err := a.patchConfigSettings(body); err != nil {
			writeError(w, 400, "config_save_failed", err.Error(), nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "settings.patch", "config_file", "myfiles", body)
		writeJSON(w, 200, map[string]any{"ok": true, "settings": configSettings(a.snapshotConfig())})
	default:
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
	}
}

func (a *App) handleAdminStorageTest(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.SettingsWrite || p.StorageSettings })
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_json", "请求体格式错误", nil)
		return
	}
	cfg := a.snapshotConfig()
	applyConfigPatch(&cfg, body)
	if err := testStorageConfig(r.Context(), cfg.Storage); err != nil {
		writeError(w, 400, "storage_test_failed", err.Error(), nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "settings.storage_test", "config_file", "myfiles", map[string]any{"mode": cfg.Storage.Mode, "chatId": cfg.Storage.ChatID})
	writeJSON(w, 200, map[string]any{"ok": true, "message": "存储通道测试通过"})
}

func (a *App) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/file/")
	if strings.HasPrefix(r.URL.Path, "/f/") {
		rest = strings.TrimPrefix(r.URL.Path, "/f/")
	}
	if strings.HasSuffix(rest, "/info") {
		id := publicFileID(strings.TrimSuffix(rest, "/info"))
		a.handlePublicFileInfo(w, r, id)
		return
	}
	if strings.HasSuffix(rest, "/confirm") {
		id := publicFileID(strings.TrimSuffix(rest, "/confirm"))
		a.handlePublicFileConfirm(w, r, id)
		return
	}
	id := publicFileID(rest)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic || f.Status != "active" {
		http.NotFound(w, r)
		return
	}
	if f.RequireConfirm {
		if c, err := r.Cookie("myfiles_file_confirm_" + id); err != nil || c.Value != "1" {
			writeError(w, 451, "confirmation_required", "访问该文件前需要确认", map[string]any{"confirmPath": publicFilePath(id, f.OriginalName) + "/confirm"})
			return
		}
	}
	if f.StorageProvider == "local" && f.StorageURL != "" {
		w.Header().Set("Content-Type", f.MIME)
		w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(f.OriginalName))
		http.ServeFile(w, r, f.StorageURL)
		return
	}
	if f.StorageProvider == "tgbots" && f.StorageFileID != "" {
		a.streamTGBotsFile(w, r, f)
		return
	}
	if f.StorageURL != "" {
		http.Redirect(w, r, f.StorageURL, http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (a *App) streamTGBotsFile(w http.ResponseWriter, r *http.Request, f myfiles.File) {
	cfg := a.snapshotConfig()
	if !storage.ValidBotToken(cfg.Storage.APIKey) {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, storage.FetchURL(cfg.Storage.UploadURL, cfg.Storage.APIKey, f.StorageFileID), nil)
	if err != nil {
		writeError(w, 500, "storage_fetch_failed", "创建回源请求失败", nil)
		return
	}
	resp, err := storage.NewTGBotsHTTPClient(cfg.Storage).Do(req)
	if err != nil {
		writeError(w, 502, "storage_fetch_failed", "存储回源失败", nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeError(w, resp.StatusCode, "storage_fetch_failed", "存储回源返回错误", nil)
		return
	}
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(f.OriginalName))
	if resp.Header.Get("Content-Length") != "" {
		w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
	}
	if f.IsPublic {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=0, no-store")
	}
	buf := make([]byte, 256*1024)
	_, _ = io.CopyBuffer(w, resp.Body, buf)
}

func (a *App) handlePublicFileInfo(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "file": map[string]any{
		"id": f.ID, "name": f.OriginalName, "mime": f.MIME, "size": f.Size, "sha256": f.SHA256,
		"imageWidth": f.ImageWidth, "imageHeight": f.ImageHeight, "requireConfirm": f.RequireConfirm, "url": publicFilePath(f.ID, f.OriginalName),
	}})
}

func (a *App) handlePublicFileConfirm(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	if _, err := myfiles.GetFile(a.db, id, false); err != nil {
		http.NotFound(w, r)
		return
	}
	f, _ := myfiles.GetFile(a.db, id, false)
	http.SetCookie(w, &http.Cookie{Name: "myfiles_file_confirm_" + id, Value: "1", Path: "/f/" + id, MaxAge: 3600, Secure: a.cfg.Security.CookieSecure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]any{"ok": true, "url": publicFilePath(id, f.OriginalName)})
}

func publicFileID(value string) string {
	id := strings.Trim(value, "/")
	if dot := strings.IndexByte(id, '.'); dot > 0 {
		id = id[:dot]
	}
	return id
}

func publicFilePath(id, name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" || len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return "/f/" + id
	}
	return "/f/" + id + ext
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request, allow func(MyfilesPermissions) bool) (*Session, bool) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return nil, false
	}
	if !allow(s.Permissions) {
		writeError(w, 403, "forbidden", "当前账户没有该操作权限", nil)
		return nil, false
	}
	return s, true
}

func (a *App) serveFrontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." {
		clean = "index.html"
	}
	clean = shortDashboardPath(clean)
	candidates := []string{
		filepath.Join(a.cfg.App.PublicDir, clean),
		filepath.Join(a.cfg.App.PublicDir, clean, "index.html"),
	}
	if strings.HasPrefix(r.URL.Path, "/uploads/") {
		candidates = append([]string{filepath.Join(a.cfg.App.PublicDir, "uploads", "index.html")}, candidates...)
	}
	if strings.HasPrefix(r.URL.Path, "/dashboard/files/") || strings.HasPrefix(r.URL.Path, "/d/f/") {
		candidates = append([]string{filepath.Join(a.cfg.App.PublicDir, "dashboard", "files", "detail", "index.html")}, candidates...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			setFrontendCacheHeaders(w, r.URL.Path, c)
			http.ServeFile(w, r, c)
			return
		}
	}
	http.NotFound(w, r)
}

func setFrontendCacheHeaders(w http.ResponseWriter, urlPath, diskPath string) {
	ext := strings.ToLower(filepath.Ext(diskPath))
	if ext == ".html" || strings.HasPrefix(urlPath, "/app/") {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	if strings.HasPrefix(urlPath, "/dashboard/") || strings.HasPrefix(urlPath, "/a/") || strings.HasPrefix(urlPath, "/d/") {
		w.Header().Set("Cache-Control", "no-store")
	}
}

func shortDashboardPath(clean string) string {
	switch strings.Trim(clean, "/") {
	case "d/files":
		return filepath.Join("dashboard", "files")
	case "a/files":
		return filepath.Join("dashboard", "admin", "files")
	case "a/audit":
		return filepath.Join("dashboard", "admin", "audit")
	case "a/settings":
		return filepath.Join("dashboard", "admin", "settings")
	default:
		return clean
	}
}

func limit(r *http.Request, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	if n > 500 {
		return 500
	}
	return n
}

func (a *App) snapshotConfig() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) currentStorage() storage.Uploader {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.storage
}

type uploadPolicy struct {
	MaxBytes              int64
	AllowedMIMETypes      []string
	AllowAnonymous        bool
	DefaultPublic         bool
	DefaultRequireConfirm bool
	DefaultRegionPolicy   string
	DefaultHotlinkPolicy  string
}

func (a *App) effectiveUploadPolicy() uploadPolicy {
	cfg := a.snapshotConfig()
	p := uploadPolicy{
		MaxBytes:              cfg.Upload.MaxBytes,
		AllowedMIMETypes:      cfg.Upload.AllowedMIMETypes,
		AllowAnonymous:        cfg.Upload.AllowAnonymous,
		DefaultPublic:         cfg.File.DefaultPublic,
		DefaultRequireConfirm: cfg.File.DefaultRequireConfirm,
		DefaultRegionPolicy:   cfg.File.DefaultRegionPolicy,
		DefaultHotlinkPolicy:  cfg.File.DefaultHotlinkPolicy,
	}
	return p
}

func configSettings(cfg config.Config) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	values := map[string]any{
		"site.brandName":             cfg.App.Name,
		"site.baseUrl":               cfg.App.BaseURL,
		"upload.maxMB":               float64(cfg.Upload.MaxBytes) / 1024 / 1024,
		"upload.allowAnonymous":      cfg.Upload.AllowAnonymous,
		"upload.allowedMimeTypes":    cfg.Upload.AllowedMIMETypes,
		"file.defaultPublic":         cfg.File.DefaultPublic,
		"file.defaultRequireConfirm": cfg.File.DefaultRequireConfirm,
		"file.defaultRegionPolicy":   cfg.File.DefaultRegionPolicy,
		"file.defaultHotlinkPolicy":  cfg.File.DefaultHotlinkPolicy,
		"storage.mode":               cfg.Storage.Mode,
		"storage.uploadUrl":          cfg.Storage.UploadURL,
		"storage.timeoutSeconds":     cfg.Storage.TimeoutSeconds,
		"storage.localDir":           cfg.Storage.LocalDir,
		"storage.chatId":             cfg.Storage.ChatID,
		"storage.apiKey":             "",
		"storage.apiKeyConfigured":   storage.ValidBotToken(cfg.Storage.APIKey),
		"cdn.baseUrl":                cfg.Storage.PublicBaseURL,
		"security.sessionCookieName": cfg.Security.SessionCookieName,
		"security.sessionTtlHours":   cfg.Security.SessionTTLHours,
		"security.cookieSecure":      cfg.Security.CookieSecure,
	}
	out := map[string]any{}
	for key, value := range values {
		out[key] = map[string]any{"value": value, "updatedAt": now}
	}
	return out
}

func (a *App) patchConfigSettings(body map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg := a.cfg
	applyConfigPatch(&cfg, body)
	if touchesStorage(body) && cfg.Storage.Mode == "tgbots" {
		if err := testStorageConfig(context.Background(), cfg.Storage); err != nil {
			return fmt.Errorf("存储通道测试失败，设置未保存：%w", err)
		}
	}
	path := a.configPath
	if path == "" {
		return fmt.Errorf("config source path is not available")
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	saved, err := config.Load(path)
	if err != nil {
		return err
	}
	a.cfg = saved
	a.configPath = saved.SourcePath
	a.storage = storage.NewUploader(saved.Storage)
	return nil
}

func applyConfigPatch(cfg *config.Config, body map[string]any) {
	for key, value := range body {
		switch key {
		case "site.brandName":
			cfg.App.Name = stringValue(value)
		case "site.baseUrl":
			cfg.App.BaseURL = stringValue(value)
		case "upload.maxMB":
			if n := floatValue(value); n > 0 {
				cfg.Upload.MaxBytes = int64(n * 1024 * 1024)
			}
		case "upload.allowAnonymous":
			cfg.Upload.AllowAnonymous = boolValue(value)
		case "upload.allowedMimeTypes":
			if list := stringSliceValue(value); len(list) > 0 {
				cfg.Upload.AllowedMIMETypes = list
			}
		case "file.defaultPublic":
			cfg.File.DefaultPublic = boolValue(value)
		case "file.defaultRequireConfirm":
			cfg.File.DefaultRequireConfirm = boolValue(value)
		case "file.defaultRegionPolicy":
			cfg.File.DefaultRegionPolicy = stringValue(value)
		case "file.defaultHotlinkPolicy":
			cfg.File.DefaultHotlinkPolicy = stringValue(value)
		case "storage.mode":
			cfg.Storage.Mode = stringValue(value)
		case "storage.uploadUrl":
			cfg.Storage.UploadURL = strings.TrimSuffix(stringValue(value), "/files")
		case "storage.timeoutSeconds":
			if n := int(floatValue(value)); n > 0 {
				cfg.Storage.TimeoutSeconds = n
			}
		case "storage.localDir":
			cfg.Storage.LocalDir = stringValue(value)
		case "storage.chatId":
			cfg.Storage.ChatID = stringValue(value)
		case "storage.apiKey":
			if s := stringValue(value); s != "" {
				cfg.Storage.APIKey = s
			}
		case "cdn.baseUrl":
			cfg.Storage.PublicBaseURL = stringValue(value)
		case "security.sessionCookieName":
			cfg.Security.SessionCookieName = stringValue(value)
		case "security.sessionTtlHours":
			if n := int(floatValue(value)); n > 0 {
				cfg.Security.SessionTTLHours = n
			}
		case "security.cookieSecure":
			cfg.Security.CookieSecure = boolValue(value)
		}
	}
}

func touchesStorage(body map[string]any) bool {
	for key := range body {
		if strings.HasPrefix(key, "storage.") {
			return true
		}
	}
	return false
}

func testStorageConfig(ctx context.Context, cfg config.StorageConfig) error {
	tmp, err := os.CreateTemp("", "myfiles-storage-test-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	content := []byte("myfiles tgbots storage test\n")
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	sha := sha256.Sum256(content)
	_, err = storage.NewUploader(cfg).Upload(ctx, storage.UploadInput{
		TempPath: tmp.Name(),
		FileID:   ids.New("tst"),
		Filename: "myfiles-storage-test.txt",
		MIME:     "text/plain",
		SHA256:   hex.EncodeToString(sha[:]),
		Size:     int64(len(content)),
	})
	return err
}

func stringValue(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func floatValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n
	default:
		return 0
	}
}

func stringSliceValue(value any) []string {
	switch v := value.(type) {
	case []any:
		out := []string{}
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		out := []string{}
		for _, item := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' }) {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (a *App) settingValue(key string) (any, bool) {
	var raw string
	if err := a.db.QueryRow(`SELECT value_json FROM site_settings WHERE key=?`, key).Scan(&raw); err != nil {
		return nil, false
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw, true
	}
	return value, true
}

func (a *App) settingBool(key string) (bool, bool) {
	value, ok := a.settingValue(key)
	if !ok {
		return false, false
	}
	v, ok := value.(bool)
	return v, ok
}

func (a *App) settingFloat(key string) (float64, bool) {
	value, ok := a.settingValue(key)
	if !ok {
		return 0, false
	}
	v, ok := value.(float64)
	return v, ok
}

func (a *App) settingString(key string) (string, bool) {
	value, ok := a.settingValue(key)
	if !ok {
		return "", false
	}
	v, ok := value.(string)
	return strings.TrimSpace(v), ok
}

func (a *App) settingStringSlice(key string) ([]string, bool) {
	value, ok := a.settingValue(key)
	if !ok {
		return nil, false
	}
	switch v := value.(type) {
	case []any:
		out := []string{}
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		out := []string{}
		for _, item := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' }) {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
