package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
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
	case p == "/api/files/batch":
		a.handleFilesBatch(w, r)
	case strings.HasPrefix(p, "/api/files/"):
		a.handleFileAPI(w, r)
	case strings.HasPrefix(p, "/api/shares/"):
		a.handleShareAPI(w, r)
	case strings.HasPrefix(p, "/api/uploads/"):
		a.handleUploadBatch(w, r)
	case strings.HasPrefix(p, "/api/pickup/"):
		a.handlePickup(w, r)
	case p == "/api/admin/files":
		a.handleAdminFiles(w, r)
	case p == "/api/admin/files/batch":
		a.handleAdminFilesBatch(w, r)
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
	case p == "/download":
		a.handleDownloadPage(w, r)
	case strings.HasPrefix(p, "/file/"), strings.HasPrefix(p, "/f/"):
		a.handlePublicFile(w, r)
	case strings.HasPrefix(p, "/pickup/"):
		a.handlePickupFile(w, r)
	case isLegacyConsolePath(p):
		http.Redirect(w, r, legacyConsoleTarget(p), http.StatusFound)
	case strings.HasPrefix(p, "/uploads"):
		a.redirectLegacyUploadResult(w, r)
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
	cfg := a.snapshotConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"brand": map[string]any{"name": cfg.App.Name, "origin": requestOrigin(r)},
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"batchId":         batch.ID,
		"pickupCode":      batch.PickupCode,
		"pickupExpiresAt": batch.PickupExpiresAt,
		"status":          status,
		"items":           items,
		"resultPath":      "/?upload=" + url.QueryEscape(batch.ID),
		"downloadPath":    "/download?upload=" + url.QueryEscape(batch.ID),
	})
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

	publicURL := a.publicBaseURL(r) + publicFilePath(fileID, fh.Filename)
	if up.PublicURL != "" && !isLocalBaseURL(up.PublicURL) {
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
	files := make([]map[string]any, 0, len(list))
	for _, f := range list {
		files = append(files, a.filePayload(f))
	}
	writeJSON(w, 200, map[string]any{"ok": true, "files": files})
}

func (a *App) handleFilesBatch(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	var body struct {
		Action  string   `json:"action"`
		FileIDs []string `json:"fileIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_json", "请求体格式错误", nil)
		return
	}
	idsList := cleanIDs(body.FileIDs, 100)
	if len(idsList) == 0 {
		writeError(w, 400, "empty_selection", "请选择文件", nil)
		return
	}
	authorized := make([]string, 0, len(idsList))
	for _, id := range idsList {
		f, err := myfiles.GetFile(a.db, id, false)
		if err != nil {
			continue
		}
		if f.OwnerUserID != s.User.ID && !s.Permissions.AllFilesWrite {
			writeError(w, 403, "forbidden", "包含无权操作的文件", nil)
			return
		}
		authorized = append(authorized, id)
	}
	if len(authorized) == 0 {
		writeError(w, 404, "not_found", "文件不存在", nil)
		return
	}
	switch body.Action {
	case "delete":
		deleted := 0
		for _, id := range authorized {
			if err := myfiles.SoftDelete(a.db, id); err == nil {
				deleted++
			}
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "file.batch.delete", "file", "", map[string]any{"count": deleted, "fileIds": authorized})
		writeJSON(w, 200, map[string]any{"ok": true, "deleted": deleted})
	case "share":
		share, err := myfiles.CreatePickupShare(a.db, s.User.ID, authorized)
		if err != nil {
			writeError(w, 500, "db_error", "创建取件码失败", nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "share.batch.create", "file", "", map[string]any{"pickupCode": share.PickupCode, "count": len(authorized), "fileIds": authorized})
		writeJSON(w, 200, map[string]any{"ok": true, "share": share, "url": "/download?code=" + url.QueryEscape(share.PickupCode)})
	default:
		writeError(w, 400, "bad_action", "不支持的批量操作", nil)
	}
}

func (a *App) handleFileAPI(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/files/")
	shareRequest := false
	if strings.HasSuffix(id, "/share") {
		id = strings.TrimSuffix(id, "/share")
		shareRequest = true
	}
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
		shares, _ := myfiles.ListPickupSharesForFile(a.db, id)
		writeJSON(w, 200, map[string]any{"ok": true, "file": a.filePayload(f), "shares": shares})
	case http.MethodPost:
		if shareRequest {
			if f.OwnerUserID != s.User.ID && !s.Permissions.AllFilesWrite {
				writeError(w, 403, "forbidden", "无权分享该文件", nil)
				return
			}
			share, err := myfiles.CreatePickupShare(a.db, s.User.ID, []string{id})
			if err != nil {
				writeError(w, 500, "db_error", "创建取件码失败", nil)
				return
			}
			audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "share.create", "file", id, map[string]any{"pickupCode": share.PickupCode})
			writeJSON(w, 200, map[string]any{"ok": true, "share": share, "url": "/download?code=" + url.QueryEscape(share.PickupCode)})
			return
		}
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
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

func (a *App) filePayload(f myfiles.File) map[string]any {
	payload := map[string]any{
		"id":              f.ID,
		"batchId":         f.BatchID,
		"ownerUserId":     f.OwnerUserID,
		"originalName":    f.OriginalName,
		"storedName":      f.StoredName,
		"mime":            f.MIME,
		"size":            f.Size,
		"sha256":          f.SHA256,
		"imageWidth":      f.ImageWidth,
		"imageHeight":     f.ImageHeight,
		"storageProvider": f.StorageProvider,
		"storageFileId":   f.StorageFileID,
		"storageUrl":      f.StorageURL,
		"publicUrl":       f.PublicURL,
		"isPublic":        f.IsPublic,
		"requireConfirm":  f.RequireConfirm,
		"regionPolicy":    f.RegionPolicy,
		"hotlinkPolicy":   f.HotlinkPolicy,
		"status":          f.Status,
		"createdAt":       f.CreatedAt,
		"updatedAt":       f.UpdatedAt,
	}
	if f.BatchID != "" {
		if b, _, err := myfiles.GetBatch(a.db, f.BatchID); err == nil && myfiles.ShareActive(myfiles.PickupShare{PickupExpiresAt: b.PickupExpiresAt}) {
			payload["uploadPickupCode"] = b.PickupCode
			payload["uploadPickupExpiresAt"] = b.PickupExpiresAt
		}
	}
	return payload
}

func (a *App) handleShareAPI(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeError(w, 401, "unauthorized", "请先使用统一账户登录", nil)
		return
	}
	code := strings.TrimPrefix(r.URL.Path, "/api/shares/")
	if r.Method != http.MethodDelete {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	share, err := myfiles.RevokePickupShare(a.db, code, s.User.ID, s.Permissions.AllFilesWrite)
	if err != nil {
		writeError(w, 404, "not_found", "分享取件码不存在或已失效", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "share.revoke", "pickup_share", share.ID, map[string]any{"pickupCode": share.PickupCode})
	writeJSON(w, 200, map[string]any{"ok": true, "share": share})
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
	if !a.canViewBatch(w, r, b) {
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "batch": b, "files": list})
}

func (a *App) canViewBatch(w http.ResponseWriter, r *http.Request, b myfiles.Batch) bool {
	s, _ := a.readSession(r)
	if b.OwnerUserID == "" {
		return true
	}
	if s == nil {
		writeError(w, 401, "unauthorized", "请先登录查看该上传批次", nil)
		return false
	}
	if s.User.ID != b.OwnerUserID && !s.Permissions.AllFilesRead {
		writeError(w, 403, "forbidden", "无权查看该上传批次", nil)
		return false
	}
	return true
}

func (a *App) handlePickup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	code := strings.TrimPrefix(r.URL.Path, "/api/pickup/")
	b, list, err := myfiles.GetBatchByPickupCode(a.db, code)
	if err == nil {
		audit.Write(a.db, r, audit.Actor{}, "pickup.read", "upload_batch", b.ID, map[string]any{"pickupCode": b.PickupCode})
		writeJSON(w, 200, map[string]any{"ok": true, "batch": b, "files": list})
		return
	}
	share, list, err := myfiles.GetShareByPickupCode(a.db, code)
	if err != nil {
		writeError(w, 404, "pickup_not_found", "取件码不存在或已过期", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{}, "pickup.read", "pickup_share", share.ID, map[string]any{"pickupCode": share.PickupCode})
	writeJSON(w, 200, map[string]any{"ok": true, "batch": shareBatch(share, len(list)), "share": share, "files": list})
}

func (a *App) handlePickupFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/pickup/"), "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	code := parts[0]
	id := publicFileID(parts[1])
	b, list, err := myfiles.GetBatchByPickupCode(a.db, code)
	targetType := "upload_batch"
	targetID := b.ID
	if err != nil {
		share, shareFiles, shareErr := myfiles.GetShareByPickupCode(a.db, code)
		if shareErr != nil {
			http.NotFound(w, r)
			return
		}
		list = shareFiles
		targetType = "pickup_share"
		targetID = share.ID
	}
	var f myfiles.File
	found := false
	for _, item := range list {
		if item.ID == id {
			f = item
			found = true
			break
		}
	}
	if !found || f.Status != "active" {
		http.NotFound(w, r)
		return
	}
	audit.Write(a.db, r, audit.Actor{}, "pickup.download", "file", f.ID, map[string]any{targetType: targetID})
	if a.needsDownloadConfirm(r, f) {
		http.Redirect(w, r, downloadPagePath(r.URL.String()), http.StatusFound)
		return
	}
	a.serveStoredFile(w, r, f, false)
}

func shareBatch(share myfiles.PickupShare, total int) myfiles.Batch {
	return myfiles.Batch{
		ID:              share.ID,
		OwnerUserID:     share.OwnerUserID,
		PickupCode:      share.PickupCode,
		PickupExpiresAt: share.PickupExpiresAt,
		Status:          "active",
		TotalFiles:      total,
		SuccessCount:    total,
		CreatedAt:       share.CreatedAt,
		UpdatedAt:       share.UpdatedAt,
	}
}

func (a *App) serveStoredFile(w http.ResponseWriter, r *http.Request, f myfiles.File, publicCache bool) {
	if f.StorageProvider == "local" && f.StorageURL != "" {
		w.Header().Set("Content-Type", f.MIME)
		w.Header().Set("Content-Disposition", contentDisposition(r, f))
		if publicCache {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "private, max-age=0, no-store")
		}
		http.ServeFile(w, r, f.StorageURL)
		return
	}
	if f.StorageProvider == "tgbots" && f.StorageFileID != "" {
		f.IsPublic = publicCache
		a.streamTGBotsFile(w, r, f)
		return
	}
	if f.StorageURL != "" {
		http.Redirect(w, r, f.StorageURL, http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (a *App) needsDownloadConfirm(r *http.Request, f myfiles.File) bool {
	if r.Method == http.MethodHead || r.URL.Query().Get("download") == "1" || strings.HasPrefix(f.MIME, "image/") {
		return false
	}
	if f.StorageProvider != "local" && f.StorageProvider != "tgbots" && f.StorageURL != "" {
		return false
	}
	return true
}

func downloadPagePath(next string) string {
	return "/download?next=" + url.QueryEscape(next)
}

func contentDisposition(r *http.Request, f myfiles.File) string {
	mode := "inline"
	if r.URL.Query().Get("download") == "1" && !strings.HasPrefix(f.MIME, "image/") {
		mode = "attachment"
	}
	return mode + "; filename=" + strconv.Quote(f.OriginalName)
}

func (a *App) downloadConfirmHTML(w http.ResponseWriter, r *http.Request, f myfiles.File) {
	next := r.URL.Query().Get("next")
	if next == "" || strings.HasPrefix(next, "/download") || strings.HasPrefix(next, "http://") || strings.HasPrefix(next, "https://") {
		http.NotFound(w, r)
		return
	}
	u, err := url.Parse(next)
	if err != nil || !strings.HasPrefix(u.Path, "/") {
		http.NotFound(w, r)
		return
	}
	q := u.Query()
	q.Set("download", "1")
	u.RawQuery = q.Encode()
	zh := strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "zh")
	lang := "en"
	title := "Confirm download"
	message := "This file is not an image. Please confirm before downloading."
	confirm := "Download"
	back := "Back home"
	if zh {
		lang = "zh-CN"
		title = "确认下载"
		message = "这个文件不是图片。为避免浏览器直接打开或误触下载，请确认后继续。"
		confirm = "确认下载"
		back = "返回首页"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0, no-store")
	fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>%s</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f6fbff;color:#182033;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
main{width:min(560px,calc(100%% - 32px));border:4px solid #182033;border-radius:8px;background:#fff;box-shadow:8px 8px 0 #182033;padding:24px;display:grid;gap:16px}
a{border:3px solid #182033;border-radius:6px;background:#ffd44d;color:#15120a;font-weight:900;padding:12px 16px;text-decoration:none;display:inline-flex;justify-content:center}
.meta{color:#5d6b82;overflow-wrap:anywhere}.actions{display:flex;gap:12px;flex-wrap:wrap}.secondary{background:#fff}
</style>
</head>
<body>
<main aria-labelledby="title">
<h1 id="title">%s</h1>
<p>%s</p>
<div class="meta"><strong>%s</strong><br>%s · %s</div>
<div class="actions"><a href="%s" download>%s</a><a class="secondary" href="/">%s</a></div>
</main>
</body>
</html>`, lang, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message), html.EscapeString(f.OriginalName), html.EscapeString(f.MIME), html.EscapeString(formatBytes(f.Size)), html.EscapeString(u.String()), html.EscapeString(confirm), html.EscapeString(back))
}

func (a *App) downloadResultHTML(w http.ResponseWriter, r *http.Request, b myfiles.Batch, files []myfiles.File, code string) {
	zh := strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "zh")
	lang := "en"
	title := "Pickup code ready"
	codeLabel := "Pickup code"
	copyText := "Copy link"
	openText := "Open"
	back := "Back home"
	valid := "Valid until"
	if zh {
		lang = "zh-CN"
		title = "取件码已生成"
		codeLabel = "取件码"
		copyText = "复制链接"
		openText = "打开"
		back = "返回首页"
		valid = "有效期至"
	}
	link := requestOrigin(r) + "/download?code=" + url.QueryEscape(code)
	var rows strings.Builder
	for _, f := range files {
		fileURL := publicFilePath(f.ID, f.OriginalName)
		if code != "" {
			fileURL = "/pickup/" + url.PathEscape(code) + "/" + url.PathEscape(f.ID) + strings.ToLower(filepath.Ext(f.OriginalName))
		}
		rows.WriteString(fmt.Sprintf(`<article class="file-row"><span class="file-badge">%s</span><div><strong>%s</strong><small>%s · %s</small></div><a href="%s" target="_blank">%s</a></article>`,
			html.EscapeString(fileIcon(f.MIME, f.OriginalName)), html.EscapeString(f.OriginalName), html.EscapeString(f.MIME), html.EscapeString(formatBytes(f.Size)), html.EscapeString(fileURL), html.EscapeString(openText)))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=0, no-store")
	fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>%s</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f6fbff;color:#182033;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
main{width:min(760px,calc(100%% - 32px));border:4px solid #182033;border-radius:8px;background:#fff;box-shadow:8px 8px 0 #182033;padding:24px;display:grid;gap:16px}
.code{display:flex;justify-content:space-between;gap:12px;align-items:center;flex-wrap:wrap;border:3px solid #182033;border-radius:8px;background:#f6fbff;padding:14px}.code strong{font-size:34px;letter-spacing:.08em}.code span,.meta,small{color:#5d6b82}.actions{display:flex;gap:12px;flex-wrap:wrap}button,a{border:3px solid #182033;border-radius:6px;background:#ffd44d;color:#15120a;font-weight:900;padding:12px 16px;text-decoration:none;display:inline-flex;justify-content:center;cursor:pointer}.secondary{background:#fff}.file-list{display:grid;gap:10px}.file-row{display:grid;grid-template-columns:56px minmax(0,1fr) auto;gap:12px;align-items:center;border:2px solid rgba(24,32,51,.22);border-radius:8px;padding:10px}.file-badge{width:52px;height:52px;display:grid;place-items:center;border:2px solid #182033;border-radius:8px;background:#dff0ff;font-size:24px;font-weight:900}.file-row div{min-width:0}.file-row strong,.file-row small{display:block;overflow-wrap:anywhere}@media(max-width:640px){.file-row{grid-template-columns:52px 1fr}.file-row a{grid-column:1/-1}}
</style>
</head>
<body>
<main aria-labelledby="title">
<h1 id="title">%s</h1>
<section class="code"><div><span>%s</span><strong>%s</strong><div class="meta">%s %s</div></div><button type="button" id="copy">%s</button></section>
<div class="actions"><a class="secondary" href="/">%s</a></div>
<section class="file-list">%s</section>
</main>
<script>
document.querySelector("#copy").addEventListener("click", async () => {
  await navigator.clipboard?.writeText(%q);
  document.querySelector("#copy").textContent = %q;
});
</script>
</body>
</html>`, lang, html.EscapeString(title), html.EscapeString(title), html.EscapeString(codeLabel), html.EscapeString(code), html.EscapeString(valid), html.EscapeString(formatTimeLabel(b.PickupExpiresAt)), html.EscapeString(copyText), html.EscapeString(back), rows.String(), link, copyText)
}

func fileIcon(mime, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "▧"
	case strings.HasPrefix(mime, "video/"):
		return "▶"
	case strings.HasPrefix(mime, "audio/"):
		return "♪"
	case strings.Contains(mime, "pdf") || ext == ".pdf":
		return "◫"
	case strings.Contains(mime, "zip") || strings.Contains(mime, "archive") || ext == ".zip" || ext == ".rar" || ext == ".7z" || ext == ".gz":
		return "▣"
	case strings.Contains(mime, "json") || strings.Contains(mime, "javascript") || strings.Contains(mime, "xml") || ext == ".js" || ext == ".json" || ext == ".html" || ext == ".css":
		return "{ }"
	case strings.Contains(mime, "spreadsheet") || ext == ".csv" || ext == ".xls" || ext == ".xlsx":
		return "▦"
	case strings.Contains(mime, "presentation") || ext == ".ppt" || ext == ".pptx":
		return "▥"
	case strings.HasPrefix(mime, "text/") || ext == ".txt" || ext == ".md" || ext == ".doc" || ext == ".docx":
		return "▤"
	default:
		return "◇"
	}
}

func formatTimeLabel(value string) string {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	return value
}

func formatBytes(n int64) string {
	if n > 1024*1024 {
		return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024)
	}
	if n > 1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
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

func (a *App) handleAdminFilesBatch(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.AllFilesRead })
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !s.Permissions.AllFilesWrite {
		writeError(w, 403, "forbidden", "无权批量管理文件", nil)
		return
	}
	var body struct {
		Action  string   `json:"action"`
		FileIDs []string `json:"fileIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_json", "请求体格式错误", nil)
		return
	}
	idsList := cleanIDs(body.FileIDs, 200)
	if len(idsList) == 0 {
		writeError(w, 400, "empty_selection", "请选择文件", nil)
		return
	}
	count := 0
	for _, id := range idsList {
		switch body.Action {
		case "delete":
			if err := myfiles.SoftDelete(a.db, id); err == nil {
				count++
			}
		case "public":
			v := true
			if err := myfiles.PatchAdmin(a.db, id, &v, nil, "", "", ""); err == nil {
				count++
			}
		case "private":
			v := false
			if err := myfiles.PatchAdmin(a.db, id, &v, nil, "", "", ""); err == nil {
				count++
			}
		case "confirm":
			v := true
			if err := myfiles.PatchAdmin(a.db, id, nil, &v, "", "", ""); err == nil {
				count++
			}
		case "no-confirm":
			v := false
			if err := myfiles.PatchAdmin(a.db, id, nil, &v, "", "", ""); err == nil {
				count++
			}
		default:
			writeError(w, 400, "bad_action", "不支持的批量操作", nil)
			return
		}
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "admin.file.batch."+body.Action, "file", "", map[string]any{"count": count, "fileIds": idsList})
	writeJSON(w, 200, map[string]any{"ok": true, "updated": count})
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

func (a *App) handleDownloadPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	if uploadID := strings.TrimSpace(r.URL.Query().Get("upload")); uploadID != "" {
		b, list, err := myfiles.GetBatch(a.db, uploadID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !a.canViewBatch(w, r, b) {
			return
		}
		a.downloadResultHTML(w, r, b, list, b.PickupCode)
		return
	}
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		b, list, err := myfiles.GetBatchByPickupCode(a.db, code)
		if err != nil {
			share, shareFiles, shareErr := myfiles.GetShareByPickupCode(a.db, code)
			if shareErr != nil {
				http.NotFound(w, r)
				return
			}
			a.downloadResultHTML(w, r, shareBatch(share, len(shareFiles)), shareFiles, share.PickupCode)
			return
		}
		a.downloadResultHTML(w, r, b, list, b.PickupCode)
		return
	}
	next := r.URL.Query().Get("next")
	u, err := url.Parse(next)
	if err != nil || !strings.HasPrefix(u.Path, "/") {
		http.NotFound(w, r)
		return
	}
	var id string
	switch {
	case strings.HasPrefix(u.Path, "/file/"):
		id = publicFileID(strings.TrimPrefix(u.Path, "/file/"))
	case strings.HasPrefix(u.Path, "/f/"):
		id = publicFileID(strings.TrimPrefix(u.Path, "/f/"))
	case strings.HasPrefix(u.Path, "/pickup/"):
		parts := strings.Split(strings.Trim(strings.TrimPrefix(u.Path, "/pickup/"), "/"), "/")
		if len(parts) >= 2 {
			id = publicFileID(parts[1])
		}
	default:
		http.NotFound(w, r)
		return
	}
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || strings.HasPrefix(f.MIME, "image/") {
		http.NotFound(w, r)
		return
	}
	a.downloadConfirmHTML(w, r, f)
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
	where := []string{"1=1"}
	args := []any{}
	if ip := strings.TrimSpace(r.URL.Query().Get("ip")); ip != "" {
		where = append(where, "ip LIKE ?")
		args = append(args, "%"+ip+"%")
	}
	args = append(args, limit(r, 50))
	rows, err := a.db.Query(`SELECT id, COALESCE(actor_account_user_id,''), COALESCE(actor_role,''), action, target_type, COALESCE(target_id,''), detail_json, COALESCE(ip,''), COALESCE(user_agent,''), created_at
		FROM audit_logs WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT ?`, args...)
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
		if s := strings.TrimSpace(stringValue(body["site.baseUrl"])); s == "" || isLocalBaseURL(s) {
			body["site.baseUrl"] = requestOrigin(r)
		}
		if s := strings.TrimSpace(stringValue(body["cdn.baseUrl"])); s == "" || isLocalBaseURL(s) {
			body["cdn.baseUrl"] = requestOrigin(r)
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
	if a.needsDownloadConfirm(r, f) {
		http.Redirect(w, r, downloadPagePath(r.URL.String()), http.StatusFound)
		return
	}
	a.serveStoredFile(w, r, f, true)
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
	w.Header().Set("Content-Disposition", contentDisposition(r, f))
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
	if ext == ".html" {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	if strings.HasPrefix(urlPath, "/app/") || strings.HasPrefix(urlPath, "/assets/") || ext == ".css" || ext == ".js" || ext == ".png" || ext == ".svg" || ext == ".webp" {
		w.Header().Set("Cache-Control", "public, max-age=604800, stale-while-revalidate=86400")
		return
	}
	if strings.HasPrefix(urlPath, "/dashboard/") || strings.HasPrefix(urlPath, "/a/") || strings.HasPrefix(urlPath, "/d/") {
		w.Header().Set("Cache-Control", "no-store")
	}
}

func isLegacyConsolePath(p string) bool {
	return p == "/d/files" || p == "/a/files" || p == "/a/audit" || p == "/a/settings" ||
		strings.HasPrefix(p, "/dashboard/files") || strings.HasPrefix(p, "/dashboard/admin/files") ||
		strings.HasPrefix(p, "/dashboard/admin/audit") || strings.HasPrefix(p, "/dashboard/admin/settings")
}

func legacyConsoleTarget(p string) string {
	switch {
	case p == "/d/files" || strings.HasPrefix(p, "/dashboard/files"):
		return "/dashboard#files"
	case p == "/a/files" || strings.HasPrefix(p, "/dashboard/admin/files"):
		return "/dashboard#admin-files"
	case p == "/a/audit" || strings.HasPrefix(p, "/dashboard/admin/audit"):
		return "/dashboard#audit"
	case p == "/a/settings" || strings.HasPrefix(p, "/dashboard/admin/settings"):
		return "/dashboard#settings"
	default:
		return "/dashboard"
	}
}

func (a *App) redirectLegacyUploadResult(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code != "" {
		http.Redirect(w, r, "/download?code="+url.QueryEscape(code), http.StatusFound)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/uploads"), "/")
	if id != "" {
		http.Redirect(w, r, "/download?upload="+url.QueryEscape(id), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
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

func cleanIDs(in []string, max int) []string {
	if max <= 0 {
		max = 100
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= max {
			break
		}
	}
	return out
}

func (a *App) publicBaseURL(r *http.Request) string {
	cfg := a.snapshotConfig()
	base := strings.TrimRight(cfg.App.BaseURL, "/")
	if base == "" || isLocalBaseURL(base) {
		return requestOrigin(r)
	}
	return base
}

func requestOrigin(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	host = strings.TrimSpace(strings.Split(host, ",")[0])
	if host == "" {
		host = "127.0.0.1"
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func isLocalBaseURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "://127.0.0.1") || strings.Contains(value, "://localhost")
}

func normalizeRegionPolicy(value string) string {
	value = strings.TrimSpace(value)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "global"
	}
	mode := strings.ToLower(strings.TrimSpace(parts[0]))
	if mode != "allow" && mode != "deny" {
		return "global"
	}
	codes := cleanRegionCodes(parts[1])
	if len(codes) == 0 {
		return "global"
	}
	return mode + ":" + strings.Join(codes, ",")
}

func cleanRegionCodes(value string) []string {
	split := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；' || r == ' ' || r == '\n' || r == '\t'
	})
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range split {
		code := strings.ToUpper(strings.TrimSpace(raw))
		code = strings.Map(func(r rune) rune {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return -1
		}, code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
		if len(out) >= 32 {
			break
		}
	}
	return out
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
		"audit.retentionDays":        cfg.Audit.RetentionDays,
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
			cfg.File.DefaultRegionPolicy = normalizeRegionPolicy(stringValue(value))
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
		case "audit.retentionDays":
			if n := int(floatValue(value)); n > 0 {
				cfg.Audit.RetentionDays = n
			}
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
