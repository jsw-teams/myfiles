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
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	mu          sync.RWMutex
	chunkMu     sync.Mutex
	videoMu     sync.Mutex
	cfg         config.Config
	configPath  string
	db          *sql.DB
	account     *account.Client
	storage     storage.Uploader
	finalizeSem chan struct{}
}

const (
	uploadChunkMinSize     = 512 * 1024
	uploadChunkDefaultSize = 4 * 1024 * 1024
	uploadChunkMaxSize     = 16 * 1024 * 1024
	chunkUploadExpiry      = 30 * time.Minute
)

func New(cfg config.Config, database *sql.DB, accountClient *account.Client, uploader storage.Uploader) *App {
	return &App{cfg: cfg, configPath: cfg.SourcePath, db: database, account: accountClient, storage: uploader, finalizeSem: make(chan struct{}, uploadFinalizeConcurrency())}
}

func uploadFinalizeConcurrency() int {
	if runtime.NumCPU() >= 8 {
		return 2
	}
	return 1
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
	case p == "/api/upload/chunk/init":
		a.handleChunkInit(w, r)
	case p == "/api/upload/chunk/cancel":
		a.handleChunkCancel(w, r)
	case p == "/api/upload/chunk/complete":
		a.handleChunkComplete(w, r)
	case strings.HasPrefix(p, "/api/upload/chunk/"):
		a.handleChunkPart(w, r)
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
	case strings.HasPrefix(p, "/files/"):
		a.handlePublicFile(w, r)
	case strings.HasPrefix(p, "/og/"):
		a.handlePublicOGImage(w, r)
	case strings.HasPrefix(p, "/pickup/"):
		a.handlePickupFile(w, r)
	case isLegacyConsolePath(p):
		http.Redirect(w, r, legacyConsoleTarget(p), http.StatusFound)
	case strings.HasPrefix(p, "/uploads"):
		a.serveFrontend(w, r)
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
		"resultPath":      "/uploads/" + url.PathEscape(batch.ID),
		"downloadPath":    "/uploads/" + url.PathEscape(batch.ID),
	})
}

type chunkUploadManifest struct {
	ID        string            `json:"id"`
	BatchID   string            `json:"batchId"`
	Owner     string            `json:"owner"`
	Role      string            `json:"role"`
	CreatedAt string            `json:"createdAt"`
	UpdatedAt string            `json:"updatedAt"`
	ExpiresAt string            `json:"expiresAt"`
	ChunkSize int64             `json:"chunkSize"`
	Files     []chunkUploadFile `json:"files"`
	Received  map[string][]bool `json:"received"`
}

type chunkUploadFile struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Type       string `json:"type,omitempty"`
	Chunks     int    `json:"chunks"`
	UploadedAt string `json:"uploadedAt,omitempty"`
}

func (a *App) handleChunkInit(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		UploadID  string `json:"uploadId"`
		ChunkSize int64  `json:"chunkSize"`
		Files     []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			Type string `json:"type"`
		} `json:"files"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "上传初始化参数无效", nil)
		return
	}
	if len(body.Files) == 0 {
		writeError(w, 400, "file_required", "没有收到文件", nil)
		return
	}
	for _, f := range body.Files {
		if f.Size < 0 || f.Size > policy.MaxBytes {
			writeError(w, 413, "upload_too_large", "文件超过当前允许的上传限制", nil)
			return
		}
	}
	owner, role := "", ""
	if s != nil {
		owner = s.User.ID
		role = s.User.Role
	}
	chunkSize := normalizeUploadChunkSize(body.ChunkSize)
	if err := a.cleanupOldChunkUploads(); err != nil {
		writeError(w, 500, "temp_create_failed", "清理临时上传目录失败", nil)
		return
	}
	if strings.TrimSpace(body.UploadID) != "" {
		a.chunkMu.Lock()
		manifest, err := a.loadChunkManifest(body.UploadID)
		if err == nil && !manifest.expired(time.Now()) && manifest.Owner == owner && manifest.chunkSize() == chunkSize && manifest.matchesFiles(body.Files) {
			manifest.touch(time.Now())
			if err := a.saveChunkManifest(manifest); err != nil {
				a.chunkMu.Unlock()
				writeError(w, 500, "temp_create_failed", "写入上传状态失败", nil)
				return
			}
			a.chunkMu.Unlock()
			writeJSON(w, 200, map[string]any{
				"ok": true, "resumed": true, "uploadId": manifest.ID, "batchId": manifest.BatchID,
				"chunkSize": manifest.chunkSize(), "received": manifest.Received, "expiresAt": manifest.ExpiresAt,
			})
			return
		}
		_ = os.RemoveAll(a.chunkUploadDir(body.UploadID))
		a.chunkMu.Unlock()
	}
	batch, err := myfiles.CreateBatch(a.db, owner)
	if err != nil {
		writeError(w, 500, "db_error", "创建上传批次失败", nil)
		return
	}
	uploadID := ids.New("upl")
	now := time.Now()
	manifest := chunkUploadManifest{
		ID: uploadID, BatchID: batch.ID, Owner: owner, Role: role,
		CreatedAt: now.UTC().Format(time.RFC3339),
		UpdatedAt: now.UTC().Format(time.RFC3339),
		ExpiresAt: now.Add(chunkUploadExpiry).UTC().Format(time.RFC3339),
		ChunkSize: chunkSize,
		Received:  map[string][]bool{},
	}
	for _, f := range body.Files {
		chunks := int((f.Size + chunkSize - 1) / chunkSize)
		if chunks == 0 {
			chunks = 1
		}
		idx := len(manifest.Files)
		manifest.Files = append(manifest.Files, chunkUploadFile{Name: filepath.Base(f.Name), Size: f.Size, Type: f.Type, Chunks: chunks})
		manifest.Received[strconv.Itoa(idx)] = make([]bool, chunks)
	}
	if err := os.MkdirAll(a.chunkUploadDir(uploadID), 0750); err != nil {
		writeError(w, 500, "temp_create_failed", "创建临时上传目录失败", nil)
		return
	}
	if err := a.saveChunkManifest(manifest); err != nil {
		writeError(w, 500, "temp_create_failed", "写入上传状态失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "uploadId": uploadID, "batchId": batch.ID, "chunkSize": manifest.chunkSize(), "received": manifest.Received, "expiresAt": manifest.ExpiresAt})
}

func (a *App) handleChunkPart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/upload/chunk/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[1] != "file" {
		writeError(w, 404, "not_found", "not found", nil)
		return
	}
	uploadID := filepath.Base(parts[0])
	fileIndex, err1 := strconv.Atoi(parts[2])
	chunkIndex, err2 := strconv.Atoi(r.URL.Query().Get("chunk"))
	if err1 != nil || err2 != nil {
		writeError(w, 400, "bad_request", "分片参数无效", nil)
		return
	}
	manifest, err := a.loadChunkManifest(uploadID)
	if err != nil {
		writeError(w, 404, "upload_not_found", "上传会话不存在或已过期", nil)
		return
	}
	if manifest.expired(time.Now()) {
		_ = os.RemoveAll(a.chunkUploadDir(uploadID))
		writeError(w, 410, "upload_expired", "上传会话已过期，请重新上传", nil)
		return
	}
	if fileIndex < 0 || fileIndex >= len(manifest.Files) || chunkIndex < 0 || chunkIndex >= manifest.Files[fileIndex].Chunks {
		writeError(w, 400, "bad_request", "分片序号无效", nil)
		return
	}
	key := strconv.Itoa(fileIndex)
	partPath := filepath.Join(a.chunkUploadDir(uploadID), fmt.Sprintf("file-%d-chunk-%d.part", fileIndex, chunkIndex))
	chunkSize := manifest.chunkSize()
	body := http.MaxBytesReader(w, r.Body, chunkSize+1024)
	out, err := os.Create(partPath)
	if err != nil {
		writeError(w, 500, "temp_create_failed", "创建临时分片失败", nil)
		return
	}
	buf := make([]byte, 1024*1024)
	n, copyErr := io.CopyBuffer(out, body, buf)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(partPath)
		writeError(w, 500, "temp_write_failed", "写入临时分片失败", nil)
		return
	}
	if n > chunkSize {
		_ = os.Remove(partPath)
		writeError(w, 413, "upload_too_large", "分片超过当前允许的上传限制", nil)
		return
	}
	a.chunkMu.Lock()
	defer a.chunkMu.Unlock()
	manifest, err = a.loadChunkManifest(uploadID)
	if err != nil {
		_ = os.Remove(partPath)
		writeError(w, 404, "upload_not_found", "上传会话不存在或已过期", nil)
		return
	}
	if manifest.expired(time.Now()) {
		_ = os.Remove(partPath)
		_ = os.RemoveAll(a.chunkUploadDir(uploadID))
		writeError(w, 410, "upload_expired", "上传会话已过期，请重新上传", nil)
		return
	}
	if fileIndex < 0 || fileIndex >= len(manifest.Files) || chunkIndex < 0 || chunkIndex >= manifest.Files[fileIndex].Chunks {
		_ = os.Remove(partPath)
		writeError(w, 400, "bad_request", "分片序号无效", nil)
		return
	}
	if _, ok := manifest.Received[key]; !ok {
		manifest.Received[key] = make([]bool, manifest.Files[fileIndex].Chunks)
	}
	manifest.Received[key][chunkIndex] = true
	manifest.touch(time.Now())
	if err := a.saveChunkManifest(manifest); err != nil {
		writeError(w, 500, "temp_write_failed", "写入上传状态失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "received": n})
}

func normalizeUploadChunkSize(size int64) int64 {
	if size <= 0 {
		return uploadChunkDefaultSize
	}
	if size < uploadChunkMinSize {
		return uploadChunkMinSize
	}
	if size > uploadChunkMaxSize {
		return uploadChunkMaxSize
	}
	rem := size % uploadChunkMinSize
	if rem != 0 {
		size -= rem
	}
	if size < uploadChunkMinSize {
		return uploadChunkMinSize
	}
	return size
}

func (m chunkUploadManifest) chunkSize() int64 {
	return normalizeUploadChunkSize(m.ChunkSize)
}

func (a *App) handleChunkComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	policy := a.effectiveUploadPolicy()
	var body struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.UploadID) == "" {
		writeError(w, 400, "bad_request", "上传完成参数无效", nil)
		return
	}
	manifest, err := a.loadChunkManifest(body.UploadID)
	if err != nil {
		writeError(w, 404, "upload_not_found", "上传会话不存在或已过期", nil)
		return
	}
	if manifest.expired(time.Now()) {
		_ = os.RemoveAll(a.chunkUploadDir(body.UploadID))
		writeError(w, 410, "upload_expired", "上传会话已过期，请重新上传", nil)
		return
	}
	a.finalizeSem <- struct{}{}
	defer func() { <-a.finalizeSem }()

	type item struct {
		OK    bool   `json:"ok"`
		File  any    `json:"file,omitempty"`
		Error string `json:"error,omitempty"`
		Code  string `json:"code,omitempty"`
		Name  string `json:"name,omitempty"`
	}
	var items []item
	success, failed := 0, 0
	for i, f := range manifest.Files {
		if !manifest.fileComplete(i) {
			failed++
			items = append(items, item{OK: false, Name: f.Name, Code: "upload_incomplete", Error: "文件分片尚未全部上传"})
			continue
		}
		tmpPath, err := a.assembleChunkFile(manifest.ID, i, f)
		if err != nil {
			failed++
			items = append(items, item{OK: false, Name: f.Name, Code: "temp_write_failed", Error: "合并分片失败"})
			continue
		}
		file, code, err := a.processTempUpload(r.Context(), r, tmpPath, manifest.BatchID, manifest.Owner, f.Name, f.Size, policy)
		_ = os.Remove(tmpPath)
		if err != nil {
			failed++
			items = append(items, item{OK: false, Name: f.Name, Code: code, Error: err.Error()})
			continue
		}
		success++
		items = append(items, item{OK: true, File: file, Name: f.Name})
	}
	status := "completed"
	if failed > 0 && success == 0 {
		status = "failed"
	} else if failed > 0 {
		status = "partial"
	}
	_ = myfiles.UpdateBatchCounts(a.db, manifest.BatchID, len(manifest.Files), success, failed, status)
	audit.Write(a.db, r, audit.Actor{AccountUserID: manifest.Owner, Role: manifest.Role}, "upload.create", "upload_batch", manifest.BatchID, map[string]any{"total": len(manifest.Files), "success": success, "failed": failed, "chunked": true})
	_ = os.RemoveAll(a.chunkUploadDir(manifest.ID))
	batch, _, _ := myfiles.GetBatch(a.db, manifest.BatchID)
	writeJSON(w, 200, map[string]any{
		"ok":              true,
		"batchId":         manifest.BatchID,
		"pickupCode":      batch.PickupCode,
		"pickupExpiresAt": batch.PickupExpiresAt,
		"status":          status,
		"items":           items,
		"resultPath":      "/uploads/" + url.PathEscape(manifest.BatchID),
		"downloadPath":    "/uploads/" + url.PathEscape(manifest.BatchID),
	})
}

func (a *App) handleChunkCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	var body struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.UploadID) == "" {
		writeError(w, 400, "bad_request", "取消上传参数无效", nil)
		return
	}
	_ = os.RemoveAll(a.chunkUploadDir(body.UploadID))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (m *chunkUploadManifest) touch(now time.Time) {
	m.UpdatedAt = now.UTC().Format(time.RFC3339)
	m.ExpiresAt = now.Add(chunkUploadExpiry).UTC().Format(time.RFC3339)
}

func (m chunkUploadManifest) expired(now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		updatedAt, err := time.Parse(time.RFC3339, m.UpdatedAt)
		if err != nil {
			updatedAt, _ = time.Parse(time.RFC3339, m.CreatedAt)
		}
		expiresAt = updatedAt.Add(chunkUploadExpiry)
	}
	return !expiresAt.IsZero() && now.After(expiresAt)
}

func (m chunkUploadManifest) matchesFiles(files []struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}) bool {
	if len(files) != len(m.Files) {
		return false
	}
	for i, f := range files {
		if filepath.Base(f.Name) != m.Files[i].Name || f.Size != m.Files[i].Size || f.Type != m.Files[i].Type {
			return false
		}
	}
	return true
}

func (m chunkUploadManifest) fileComplete(index int) bool {
	received := m.Received[strconv.Itoa(index)]
	if index < 0 || index >= len(m.Files) || len(received) != m.Files[index].Chunks {
		return false
	}
	for _, ok := range received {
		if !ok {
			return false
		}
	}
	return true
}

func (a *App) chunkUploadRoot() string {
	return filepath.Join(a.cfg.App.TempDir, "chunks")
}

func (a *App) chunkUploadDir(uploadID string) string {
	return filepath.Join(a.chunkUploadRoot(), filepath.Base(uploadID))
}

func (a *App) chunkManifestPath(uploadID string) string {
	return filepath.Join(a.chunkUploadDir(uploadID), "manifest.json")
}

func (a *App) saveChunkManifest(manifest chunkUploadManifest) error {
	if err := os.MkdirAll(a.chunkUploadDir(manifest.ID), 0750); err != nil {
		return err
	}
	tmp := a.chunkManifestPath(manifest.ID) + ".tmp"
	b, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.chunkManifestPath(manifest.ID))
}

func (a *App) loadChunkManifest(uploadID string) (chunkUploadManifest, error) {
	var manifest chunkUploadManifest
	b, err := os.ReadFile(a.chunkManifestPath(filepath.Base(uploadID)))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (a *App) assembleChunkFile(uploadID string, fileIndex int, f chunkUploadFile) (string, error) {
	tmpPath := filepath.Join(a.chunkUploadDir(uploadID), fmt.Sprintf("file-%d-complete.upload", fileIndex))
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	var written int64
	buf := make([]byte, 256*1024)
	for i := 0; i < f.Chunks; i++ {
		partPath := filepath.Join(a.chunkUploadDir(uploadID), fmt.Sprintf("file-%d-chunk-%d.part", fileIndex, i))
		in, err := os.Open(partPath)
		if err != nil {
			_ = out.Close()
			return "", err
		}
		n, copyErr := io.CopyBuffer(out, in, buf)
		_ = in.Close()
		if copyErr != nil {
			_ = out.Close()
			return "", copyErr
		}
		_ = os.Remove(partPath)
		written += n
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if written != f.Size {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("assembled size mismatch")
	}
	return tmpPath, nil
}

func (a *App) cleanupOldChunkUploads() error {
	root := a.chunkUploadRoot()
	if err := os.MkdirAll(root, 0750); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	now := time.Now()
	fallbackCutoff := now.Add(-chunkUploadExpiry)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		manifest, err := a.loadChunkManifest(entry.Name())
		if err == nil && manifest.expired(now) {
			_ = os.RemoveAll(dir)
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(fallbackCutoff) {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
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

	n, err := io.Copy(tmp, io.LimitReader(src, policy.MaxBytes+1))
	if closeErr := tmp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return myfiles.File{}, "temp_write_failed", fmt.Errorf("写入临时文件失败")
	}
	if n > policy.MaxBytes {
		return myfiles.File{}, "upload_too_large", fmt.Errorf("文件超过当前允许的上传限制")
	}

	return a.processTempUpload(r.Context(), r, tmpPath, batchID, owner, filepath.Base(fh.Filename), n, policy)
}

func (a *App) processTempUpload(ctx context.Context, r *http.Request, tmpPath, batchID, owner, originalName string, size int64, policy uploadPolicy) (myfiles.File, string, error) {
	if size > policy.MaxBytes {
		return myfiles.File{}, "upload_too_large", fmt.Errorf("文件超过当前允许的上传限制")
	}

	h, err := fileSHA256(tmpPath)
	if err != nil {
		return myfiles.File{}, "temp_write_failed", fmt.Errorf("写入临时文件失败")
	}

	mimeType, err := detectMIME(tmpPath, originalName)
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
	fileID := ids.New("fil")
	name := filepath.Base(originalName)
	shaHex := hex.EncodeToString(h)

	up, err := a.currentStorage().Upload(ctx, storage.UploadInput{TempPath: tmpPath, FileID: fileID, Filename: name, MIME: mimeType, SHA256: shaHex, Size: size})
	if err != nil {
		return myfiles.File{}, "storage_upload_failed", fmt.Errorf("存储通道上传失败：%v", err)
	}

	publicURL := a.publicBaseURL(r) + publicFilePath(fileID, name)
	if up.PublicURL != "" && !isLocalBaseURL(up.PublicURL) {
		publicURL = up.PublicURL
	}
	f, err := myfiles.CreateFile(a.db, myfiles.CreateFileInput{
		ID: fileID, BatchID: batchID, OwnerUserID: owner, OriginalName: name, StoredName: up.FileID,
		MIME: mimeType, Size: size, SHA256: shaHex, ImageWidth: width, ImageHeight: height,
		StorageProvider: up.Provider, StorageFileID: up.FileID, ThumbnailFileID: up.ThumbnailFileID, StorageURL: up.URL, PublicURL: publicURL,
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

func fileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func detectMIME(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	if n == 0 {
		if extMIME := mimeFromName(name); extMIME != "" {
			return extMIME, nil
		}
		return "application/octet-stream", nil
	}
	detected := http.DetectContentType(buf[:n])
	extMIME := mimeFromName(name)
	if shouldPreferExtensionMIME(detected, extMIME) {
		return extMIME, nil
	}
	return detected, nil
}

func shouldPreferExtensionMIME(detected, extMIME string) bool {
	if extMIME == "" {
		return false
	}
	detected = strings.ToLower(strings.TrimSpace(detected))
	extMIME = strings.ToLower(strings.TrimSpace(extMIME))
	if detected == "application/octet-stream" {
		return true
	}
	if detected == "text/plain; charset=utf-8" && (isSubtitleMIME(extMIME) || strings.HasPrefix(extMIME, "audio/") || extMIME == "image/svg+xml") {
		return true
	}
	return (strings.HasPrefix(extMIME, "video/") || strings.HasPrefix(extMIME, "audio/")) && !strings.HasPrefix(detected, strings.Split(extMIME, "/")[0]+"/")
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
		writeJSON(w, 200, map[string]any{"ok": true, "share": share, "url": "/?code=" + url.QueryEscape(share.PickupCode)})
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
			writeJSON(w, 200, map[string]any{"ok": true, "share": share, "url": "/?code=" + url.QueryEscape(share.PickupCode)})
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
		"mime":            effectiveFileMIME(f),
		"size":            f.Size,
		"sha256":          f.SHA256,
		"imageWidth":      f.ImageWidth,
		"imageHeight":     f.ImageHeight,
		"storageProvider": f.StorageProvider,
		"storageFileId":   f.StorageFileID,
		"storageUrl":      f.StorageURL,
		"publicUrl":       canonicalPublicURL(f),
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/pickup/"), "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	code := parts[0]
	raw := false
	filePart := parts[1]
	actionIndex := 2
	if parts[1] == "raw" {
		if len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		raw = true
		filePart = parts[2]
		actionIndex = 3
	}
	id := publicFileID(filePart)
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
	fileURL := pickupFilePath(code, f.ID, f.OriginalName)
	rawFileURL := pickupRawFilePath(code, f.ID, f.OriginalName)
	downloadURL := fileURL + "/download"
	if raw {
		a.serveStoredFile(w, r, f, false)
		return
	}
	if len(parts) > actionIndex && parts[actionIndex] == "download-confirm" {
		if isPreviewableMedia(f.MIME, f.OriginalName) {
			http.Redirect(w, r, fileURL, http.StatusSeeOther)
			return
		}
		a.confirmDownload(w, r, downloadConfirmCookieName("pickup", code, f.ID), "/pickup")
		http.Redirect(w, r, downloadURL, http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	if len(parts) > actionIndex && parts[actionIndex] == "download" {
		if isPreviewableMedia(f.MIME, f.OriginalName) {
			http.Redirect(w, r, fileURL, http.StatusSeeOther)
			return
		}
		if !a.hasDownloadConfirmation(r, downloadConfirmCookieName("pickup", code, f.ID)) {
			http.Redirect(w, r, fileURL, http.StatusSeeOther)
			return
		}
		a.clearDownloadConfirmation(w, r, downloadConfirmCookieName("pickup", code, f.ID), "/pickup")
		a.serveStoredFile(w, r, f, false)
		return
	}
	if shouldServeFileBytes(r, f) {
		redirectNoStore(w, r, rawFileURL)
		return
	}
	a.previewHTML(w, r, f, false, previewLinks{
		MediaURL:    rawFileURL,
		PreviewURL:  requestOrigin(r) + fileURL,
		DownloadURL: fileURL + "/download-confirm",
	})
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
		w.Header().Set("Content-Type", effectiveFileMIME(f))
		w.Header().Set("Content-Disposition", contentDisposition(r, f))
		setStoredFileCacheHeaders(w, f, publicCache)
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

func (a *App) serveFaststartVideo(w http.ResponseWriter, r *http.Request, f myfiles.File) {
	if !shouldUseFaststartPreview(f) {
		a.serveStoredFile(w, r, f, true)
		return
	}
	path, err := a.faststartVideoPath(r.Context(), f)
	if err != nil {
		log.Printf("faststart preview unavailable: id=%s err=%v", f.ID, err)
		a.serveStoredFile(w, r, f, true)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", contentDisposition(r, f))
	setStoredFileCacheHeaders(w, f, true)
	http.ServeFile(w, r, path)
}

type mseManifest struct {
	MIME     string       `json:"mime"`
	Init     string       `json:"init"`
	Segments []mseSegment `json:"segments"`
}

type mseSegment struct {
	URL      string  `json:"url"`
	Duration float64 `json:"duration"`
}

func (a *App) handlePublicMSEVideo(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id := publicFileID(parts[0])
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic || f.Status != "active" || !shouldUseMSEPreview(f) {
		http.NotFound(w, r)
		return
	}
	dir, manifest, err := a.mseVideoManifest(r.Context(), f)
	if err != nil {
		log.Printf("mse preview unavailable: id=%s err=%v", f.ID, err)
		http.NotFound(w, r)
		return
	}
	name := parts[1]
	switch name {
	case "manifest.json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		setStoredFileCacheHeaders(w, f, true)
		_ = json.NewEncoder(w).Encode(manifest)
	case "init.mp4":
		w.Header().Set("Content-Type", "video/mp4")
		setStoredFileCacheHeaders(w, f, true)
		http.ServeFile(w, r, filepath.Join(dir, name))
	default:
		if !strings.HasPrefix(name, "seg_") || !strings.HasSuffix(name, ".m4s") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/iso.segment")
		setStoredFileCacheHeaders(w, f, true)
		http.ServeFile(w, r, filepath.Join(dir, name))
	}
}

func (a *App) mseVideoManifest(ctx context.Context, f myfiles.File) (string, mseManifest, error) {
	cfg := a.snapshotConfig()
	version := strings.Trim(fileEntityTag(f), `"`)
	cacheDir := filepath.Join(cfg.App.DataDir, "video-cache", "mse", f.ID+"-"+version)
	playlist := filepath.Join(cacheDir, "index.m3u8")
	manifestPath := filepath.Join(cacheDir, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest mseManifest
		if json.Unmarshal(data, &manifest) == nil && len(manifest.Segments) > 0 {
			return cacheDir, manifest, nil
		}
	}

	a.videoMu.Lock()
	defer a.videoMu.Unlock()
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest mseManifest
		if json.Unmarshal(data, &manifest) == nil && len(manifest.Segments) > 0 {
			return cacheDir, manifest, nil
		}
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", mseManifest{}, err
	}
	inputPath, cleanup, err := a.faststartInputPath(ctx, f)
	if err != nil {
		return "", mseManifest{}, err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-hide_banner", "-loglevel", "error", "-i", inputPath, "-c", "copy", "-f", "hls", "-hls_time", "4", "-hls_segment_type", "fmp4", "-hls_playlist_type", "vod", "-hls_flags", "independent_segments", "-hls_fmp4_init_filename", "init.mp4", "-hls_segment_filename", filepath.Join(cacheDir, "seg_%05d.m4s"), playlist)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", mseManifest{}, fmt.Errorf("ffmpeg fmp4 failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	codecs, err := mp4Codecs(ctx, inputPath)
	if err != nil {
		return "", mseManifest{}, err
	}
	segments, err := parseMSEPlaylist(playlist, f.ID, version)
	if err != nil {
		return "", mseManifest{}, err
	}
	manifest := mseManifest{
		MIME:     `video/mp4; codecs="` + strings.Join(codecs, ", ") + `"`,
		Init:     "/files/mse/" + url.PathEscape(f.ID) + "/init.mp4?v=" + url.QueryEscape(version),
		Segments: segments,
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return "", mseManifest{}, err
	}
	return cacheDir, manifest, nil
}

func parseMSEPlaylist(path, id, version string) ([]mseSegment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var duration float64
	var segments []mseSegment
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#EXTINF:") {
			value := strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ",")
			duration, _ = strconv.ParseFloat(value, 64)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ".m4s") {
			segments = append(segments, mseSegment{
				URL:      "/files/mse/" + url.PathEscape(id) + "/" + url.PathEscape(line) + "?v=" + url.QueryEscape(version),
				Duration: duration,
			})
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("empty mse playlist")
	}
	return segments, nil
}

func mp4Codecs(ctx context.Context, source string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_streams", "-of", "json", source)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var probed struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			CodecType string `json:"codec_type"`
			Profile   string `json:"profile"`
			Level     int    `json:"level"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, err
	}
	var codecs []string
	for _, stream := range probed.Streams {
		switch stream.CodecType {
		case "video":
			if stream.CodecName == "h264" {
				codecs = append(codecs, h264CodecString(stream.Profile, stream.Level))
			}
		case "audio":
			if stream.CodecName == "aac" {
				codecs = append(codecs, "mp4a.40.2")
			}
		}
	}
	if len(codecs) == 0 {
		return nil, fmt.Errorf("unsupported mp4 codecs")
	}
	return codecs, nil
}

func h264CodecString(profile string, level int) string {
	profileHex := "42E0"
	switch strings.ToLower(profile) {
	case "main":
		profileHex = "4D40"
	case "high":
		profileHex = "6400"
	}
	if level <= 0 {
		level = 30
	}
	return fmt.Sprintf("avc1.%s%02X", profileHex, level)
}

func (a *App) faststartVideoPath(ctx context.Context, f myfiles.File) (string, error) {
	cfg := a.snapshotConfig()
	cacheDir := filepath.Join(cfg.App.DataDir, "video-cache", "faststart")
	cacheName := f.ID + "-" + strings.Trim(fileEntityTag(f), `"`) + ".mp4"
	outPath := filepath.Join(cacheDir, cacheName)
	if st, err := os.Stat(outPath); err == nil && st.Size() > 0 {
		return outPath, nil
	}

	a.videoMu.Lock()
	defer a.videoMu.Unlock()
	if st, err := os.Stat(outPath); err == nil && st.Size() > 0 {
		return outPath, nil
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	inputPath, cleanup, err := a.faststartInputPath(ctx, f)
	if err != nil {
		return "", err
	}
	defer cleanup()
	tmpOut := outPath + ".tmp"
	_ = os.Remove(tmpOut)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-hide_banner", "-loglevel", "error", "-i", inputPath, "-c", "copy", "-movflags", "+faststart", "-f", "mp4", tmpOut)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmpOut)
		return "", fmt.Errorf("ffmpeg faststart failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmpOut, outPath); err != nil {
		_ = os.Remove(tmpOut)
		return "", err
	}
	return outPath, nil
}

func (a *App) faststartInputPath(ctx context.Context, f myfiles.File) (string, func(), error) {
	if f.StorageProvider == "local" && f.StorageURL != "" {
		return f.StorageURL, func() {}, nil
	}
	tmpDir := a.snapshotConfig().App.TempDir
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", func() {}, err
	}
	tmp, err := os.CreateTemp(tmpDir, "faststart-source-*.mp4")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.Remove(tmp.Name())
	}
	defer tmp.Close()

	var resp *http.Response
	switch {
	case f.StorageProvider == "tgbots" && f.StorageFileID != "":
		cfg := a.snapshotConfig()
		if !storage.ValidBotToken(cfg.Storage.APIKey) {
			cleanup()
			return "", func() {}, fmt.Errorf("invalid storage token")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, storage.FetchURL(cfg.Storage.UploadURL, cfg.Storage.APIKey, f.StorageFileID), nil)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		resp, err = storage.NewTGBotsHTTPClient(cfg.Storage).Do(req)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
	case f.StorageURL != "":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.StorageURL, nil)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
	default:
		cleanup()
		return "", func() {}, fmt.Errorf("no storage source")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cleanup()
		return "", func() {}, fmt.Errorf("source returned %s", resp.Status)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp.Name(), cleanup, nil
}

func contentDisposition(r *http.Request, f myfiles.File) string {
	mode := "inline"
	if strings.HasSuffix(strings.Trim(r.URL.Path, "/"), "/download") {
		mode = "attachment"
	}
	return mode + "; filename=" + strconv.Quote(f.OriginalName)
}

func setStoredFileCacheHeaders(w http.ResponseWriter, f myfiles.File, publicCache bool) {
	if publicCache {
		w.Header().Set("Cache-Control", "public, max-age=31536000, s-maxage=31536000, immutable")
		if etag := fileEntityTag(f); etag != "" {
			w.Header().Set("ETag", etag)
		}
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=0, no-store")
}

func fileEntityTag(f myfiles.File) string {
	if f.SHA256 != "" {
		return strconv.Quote(f.SHA256)
	}
	if f.ID != "" && f.Size >= 0 {
		return strconv.Quote(fmt.Sprintf("%s-%d", f.ID, f.Size))
	}
	return ""
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
		w.Header().Set("Content-Type", effectiveFileMIME(f))
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
	rest := strings.TrimPrefix(r.URL.Path, "/files/")
	raw := strings.HasPrefix(rest, "raw/")
	if raw {
		rest = strings.TrimPrefix(rest, "raw/")
	}
	if strings.HasSuffix(rest, "/raw") {
		raw = true
		rest = strings.TrimSuffix(rest, "/raw")
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
	if strings.HasSuffix(rest, "/download-confirm") {
		id := publicFileID(strings.TrimSuffix(rest, "/download-confirm"))
		a.handlePublicFileDownloadConfirm(w, r, id)
		return
	}
	if strings.HasSuffix(rest, "/download") {
		id := publicFileID(strings.TrimSuffix(rest, "/download"))
		a.handlePublicFileDownload(w, r, id)
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
	if raw {
		a.serveStoredFile(w, r, f, true)
		return
	}
	if shouldServeFileBytes(r, f) {
		redirectNoStore(w, r, publicRawFilePath(f.ID, f.OriginalName))
		return
	}
	a.publicPreviewHTML(w, r, f, false)
}

func isSocialPreviewRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	ua := strings.ToLower(r.UserAgent())
	if ua == "" {
		return false
	}
	for _, marker := range []string{
		"facebookexternalhit",
		"facebot",
		"twitterbot",
		"telegrambot",
		"discordbot",
		"slackbot",
		"linkedinbot",
		"whatsapp",
		"skypeuripreview",
		"pinterest",
	} {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}

func shouldServeFileBytes(r *http.Request, f myfiles.File) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	if r.Method == http.MethodHead && !strings.Contains(accept, "text/html") {
		return true
	}
	if r.Header.Get("Range") != "" {
		return true
	}
	if !isPreviewableMedia(f.MIME, f.OriginalName) {
		return false
	}
	if accept == "" {
		return false
	}
	if strings.Contains(accept, "text/html") {
		return false
	}
	return strings.Contains(accept, "*/*") || strings.Contains(accept, "image/") || strings.Contains(accept, "video/") || strings.Contains(accept, "audio/")
}

func isPreviewableMedia(mimeType, name string) bool {
	kind := mediaKind(mimeType, name)
	return kind == "image" || kind == "video" || kind == "audio"
}

func mediaKind(mimeType, name string) string {
	mimeType = effectiveMIME(mimeType, name)
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return "file"
	}
}

func effectiveFileMIME(f myfiles.File) string {
	return effectiveMIME(f.MIME, f.OriginalName)
}

func effectiveMIME(mimeType, name string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	extMIME := mimeFromName(name)
	if shouldPreferExtensionMIME(mimeType, extMIME) {
		return extMIME
	}
	if mimeType == "" {
		if extMIME != "" {
			return extMIME
		}
		return "application/octet-stream"
	}
	return mimeType
}

func mimeFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".ogv":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	case ".oga", ".ogg", ".opus":
		return "audio/ogg"
	case ".vtt":
		return "text/vtt; charset=utf-8"
	case ".srt":
		return "application/x-subrip"
	case ".lrc":
		return "text/plain; charset=utf-8"
	}
	if ext == "" {
		return ""
	}
	if v := mime.TypeByExtension(ext); v != "" {
		return v
	}
	return ""
}

func isSubtitleMIME(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "text/vtt") || mimeType == "application/x-subrip"
}

func pickupFilePath(code, id, name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" || len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return "/pickup/" + url.PathEscape(code) + "/" + url.PathEscape(id)
	}
	return "/pickup/" + url.PathEscape(code) + "/" + url.PathEscape(id) + ext
}

func pickupRawFilePath(code, id, name string) string {
	extPath := strings.TrimPrefix(pickupFilePath(code, id, name), "/pickup/"+url.PathEscape(code)+"/")
	return "/pickup/" + url.PathEscape(code) + "/raw/" + extPath
}

func (a *App) publicPreviewHTML(w http.ResponseWriter, r *http.Request, f myfiles.File, embed bool) {
	fileURL := publicFilePath(f.ID, f.OriginalName)
	rawFileURL := publicRawFilePath(f.ID, f.OriginalName)
	a.previewHTML(w, r, f, embed, previewLinks{
		MediaURL:    rawFileURL,
		PreviewURL:  requestOrigin(r) + fileURL,
		EmbedURL:    requestOrigin(r) + fileURL,
		DownloadURL: fileURL + "/download-confirm",
	})
}

func shouldUseFaststartPreview(f myfiles.File) bool {
	return mediaKind(f.MIME, f.OriginalName) == "video" && strings.EqualFold(filepath.Ext(f.OriginalName), ".mp4")
}

func shouldUseMSEPreview(f myfiles.File) bool {
	return shouldUseFaststartPreview(f)
}

type previewLinks struct {
	MediaURL    string
	PreviewURL  string
	EmbedURL    string
	DownloadURL string
}

func (a *App) previewHTML(w http.ResponseWriter, r *http.Request, f myfiles.File, embed bool, links previewLinks) {
	kind := mediaKind(f.MIME, f.OriginalName)
	contentType := effectiveFileMIME(f)
	if !isPreviewableMedia(f.MIME, f.OriginalName) {
		kind = "file"
	}
	origin := requestOrigin(r)
	mediaURL := links.MediaURL
	absMediaURL := origin + mediaURL
	previewURL := links.PreviewURL
	embedURL := links.EmbedURL
	title := f.OriginalName
	if title == "" {
		title = f.ID
	}
	description := fmt.Sprintf("%s · %s", contentType, formatBytes(f.Size))
	ogDescription := description
	if kind != "file" {
		ogDescription = title
	}
	robots := "index,follow"
	if f.RequireConfirm {
		robots = "noindex,nofollow"
	}
	if embed {
		robots = "noindex,follow"
	}
	ogType := "website"
	if kind == "image" {
		ogType = "image"
	}
	if kind == "video" {
		ogType = "video.other"
	}

	var media strings.Builder
	switch kind {
	case "image":
		width, height := imagePreviewSize(f)
		fmt.Fprintf(&media, `<div class="pswp-gallery"><a href="%s" data-pswp-width="%d" data-pswp-height="%d"><img src="%s" alt="%s" loading="eager"></a></div>`, html.EscapeString(mediaURL), width, height, html.EscapeString(mediaURL), html.EscapeString(title))
	case "video":
		poster := strings.TrimSpace(r.URL.Query().Get("poster"))
		if poster == "" {
			poster = publicOGImagePath(f.ID, f.OriginalName)
		}
		posterAttr := ""
		if poster != "" {
			posterAttr = fmt.Sprintf(` poster="%s"`, html.EscapeString(poster))
		}
		fmt.Fprintf(&media, `<div class="video-shell"><video data-caption-media data-audio-track-media controls playsinline preload="metadata"%s><source src="%s" type="%s"></video><div class="caption-overlay" data-caption-live aria-live="polite"></div></div><div class="native-audio-tools" data-native-audio-tools><label for="native-audio-track">音轨</label><select id="native-audio-track" data-native-audio-select><option value="">原生音轨</option></select></div>%s`, posterAttr, html.EscapeString(mediaURL), html.EscapeString(contentType), captionControls())
	case "audio":
		fmt.Fprintf(&media, `<div class="audio-card"><div class="audio-art">%s</div><strong>%s</strong><audio data-caption-media controls preload="metadata"><source src="%s" type="%s"></audio><div class="caption-panel" data-caption-live aria-live="polite"></div>%s</div>`, html.EscapeString(fileIcon(f.MIME, f.OriginalName)), html.EscapeString(title), html.EscapeString(mediaURL), html.EscapeString(contentType), captionControls())
	default:
		downloadText := "Download after safety check"
		message := "This file cannot be previewed online. Confirm the source is trustworthy and the file is safe before downloading."
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "zh") {
			downloadText = "确认安全后下载"
			message = "该文件无法在线预览。请确认文件来源可信、安全后再下载查看。"
		}
		fmt.Fprintf(&media, `<section class="file-card"><span class="file-icon">%s</span><h2>%s</h2><p>%s</p><dl><div><dt>MIME</dt><dd>%s</dd></div><div><dt>Size</dt><dd>%s</dd></div></dl><form method="post" action="%s"><button class="download-primary" type="submit">%s</button></form></section>`, html.EscapeString(fileIcon(f.MIME, f.OriginalName)), html.EscapeString(title), html.EscapeString(message), html.EscapeString(contentType), html.EscapeString(formatBytes(f.Size)), html.EscapeString(links.DownloadURL), html.EscapeString(downloadText))
	}

	var extraMeta strings.Builder
	if kind == "image" {
		fmt.Fprintf(&extraMeta, `<meta property="og:image" content="%s">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:image" content="%s">`, html.EscapeString(absMediaURL), html.EscapeString(absMediaURL))
	}
	if kind == "video" && embedURL != "" {
		ogImage := origin + publicOGImagePath(f.ID, f.OriginalName)
		if poster := strings.TrimSpace(r.URL.Query().Get("poster")); poster != "" {
			if strings.HasPrefix(poster, "https://") || strings.HasPrefix(poster, "http://") {
				ogImage = poster
			} else if strings.HasPrefix(poster, "/") {
				ogImage = origin + poster
			}
		}
		fmt.Fprintf(&extraMeta, `<meta property="og:video" content="%s">
<meta property="og:video:secure_url" content="%s">
<meta property="og:video:type" content="%s">
<meta property="og:image" content="%s">
<meta name="twitter:card" content="player">
<meta name="twitter:player" content="%s">
<meta name="twitter:player:width" content="1280">
<meta name="twitter:player:height" content="720">
<meta name="twitter:image" content="%s">`, html.EscapeString(absMediaURL), html.EscapeString(absMediaURL), html.EscapeString(contentType), html.EscapeString(ogImage), html.EscapeString(embedURL), html.EscapeString(ogImage))
	}
	if kind == "audio" {
		ogImage := origin + publicOGImagePath(f.ID, f.OriginalName)
		fmt.Fprintf(&extraMeta, `<meta property="og:audio" content="%s">
<meta property="og:audio:type" content="%s">
<meta property="og:image" content="%s">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:image" content="%s">`, html.EscapeString(absMediaURL), html.EscapeString(contentType), html.EscapeString(ogImage), html.EscapeString(ogImage))
	}

	bodyClass := "preview"
	if embed {
		bodyClass = "embed"
	}
	chrome := ""
	if !embed {
		chrome = fmt.Sprintf(`<header><div><h1>%s</h1></div></header>`, html.EscapeString(title))
	}
	stylesheets := ""
	if kind == "image" {
		stylesheets += `<link rel="stylesheet" href="/vendor/photoswipe/photoswipe.css">`
	}
	scripts := previewEnhancementScript(kind, previewSubtitleURL(r))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept, Range, User-Agent")
	fmt.Fprintf(w, `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="%s">
<title>%s</title>
<meta name="description" content="%s">
<link rel="canonical" href="%s">
<meta property="og:type" content="%s">
<meta property="og:title" content="%s">
<meta property="og:description" content="%s">
<meta property="og:url" content="%s">
<meta name="twitter:title" content="%s">
<meta name="twitter:description" content="%s">
%s
%s
<style>
*{box-sizing:border-box}html,body{margin:0;min-height:100%%;background:#0f1115;color:#f7f3e8;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}body.preview{min-height:100vh;display:grid;grid-template-rows:auto 1fr;background:#11151b}header{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:18px 24px;border-bottom:1px solid rgba(255,255,255,.12);background:#171c23}header div{min-width:0}span{display:block;color:#9eb5c7;font-size:13px;font-weight:800;text-transform:uppercase;letter-spacing:.08em}h1{margin:0 0 4px;font-size:clamp(20px,3vw,34px);line-height:1.15;overflow-wrap:anywhere}h2{margin:0;font-size:22px;line-height:1.2;overflow-wrap:anywhere}p{margin:0;color:#bec8d2;font-size:14px}form{margin:0}a,button{border:1px solid rgba(255,255,255,.28);border-radius:6px;color:#f7f3e8;background:transparent;text-decoration:none;padding:9px 12px;font:inherit;font-weight:800;cursor:pointer}.stage{width:100%%;min-height:0;display:grid;place-items:center;gap:14px;padding:20px;contain:layout paint}.embed .stage{height:100vh;padding:0}.pswp-gallery{width:100%%;display:grid;place-items:center}.pswp-gallery a{display:block;border:0;padding:0;line-height:0}.pswp-gallery img{display:block;max-width:100%%;max-height:calc(100vh - 132px);object-fit:contain;background:#06080b}.video-shell{position:relative;width:min(100%%,1280px);contain:layout paint}.video-shell video{display:block;width:100%%;max-height:calc(100vh - 188px);aspect-ratio:16/9;object-fit:contain;background:#06080b;border-radius:8px}.embed .video-shell,.embed video{width:100%%;height:100vh;max-height:none;border-radius:0}.caption-overlay{position:absolute;left:50%%;bottom:18px;transform:translateX(-50%%);max-width:min(92%%,980px);padding:7px 12px;border-radius:6px;background:rgba(0,0,0,.68);color:#fff;font-size:clamp(16px,2vw,24px);line-height:1.35;text-align:center;white-space:pre-wrap;pointer-events:none}.caption-overlay:empty{display:none}.caption-tools,.native-audio-tools{width:min(100%%,720px);display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:8px}.native-audio-tools{grid-template-columns:auto minmax(0,1fr);align-items:center}.native-audio-tools label{color:#9eb5c7;font-size:13px;font-weight:800;text-transform:uppercase;letter-spacing:.08em}.native-audio-tools select,.caption-tools input[type=url],.caption-tools input[type=file]{min-width:0;border:1px solid rgba(255,255,255,.22);border-radius:6px;background:#10151b;color:#f7f3e8;padding:9px 10px;font:inherit}.caption-file{display:grid;place-items:center;border:1px solid rgba(255,255,255,.28);border-radius:6px;padding:0 12px;font-weight:800;cursor:pointer}.caption-file input{position:absolute;inline-size:1px;block-size:1px;opacity:0}.audio-card,.file-card{width:min(720px,calc(100%% - 32px));display:grid;gap:16px;border:1px solid rgba(255,255,255,.16);border-radius:8px;background:#171c23;padding:20px}.audio-art{width:92px;height:92px;display:grid;place-items:center;border:1px solid rgba(255,255,255,.22);border-radius:8px;background:#222b35;color:#ffd44d;font-size:32px;font-weight:900}.audio-card strong{overflow-wrap:anywhere}.audio-card audio{width:100%%}.caption-panel{min-height:58px;display:grid;place-items:center;border:1px solid rgba(255,255,255,.12);border-radius:8px;background:#10151b;color:#f7f3e8;padding:12px;text-align:center;white-space:pre-wrap;line-height:1.45}.caption-panel:empty{display:none}.file-icon{width:64px;height:64px;display:grid;place-items:center;border:1px solid rgba(255,255,255,.24);border-radius:8px;background:#222b35;color:#ffd44d;font-size:24px;font-weight:900}.file-card dl{display:grid;gap:8px;margin:0}.file-card div{display:grid;grid-template-columns:72px minmax(0,1fr);gap:10px}.file-card dt{color:#9eb5c7;font-weight:800}.file-card dd{margin:0;overflow-wrap:anywhere}.download-primary{width:max-content;background:#ffd44d;color:#15120a;border-color:#ffd44d}@media(max-width:720px){header{align-items:flex-start;flex-direction:column}.stage{padding:12px}.pswp-gallery img,.video-shell video{max-height:calc(100vh - 210px)}.caption-tools{grid-template-columns:1fr 1fr}.caption-tools input[type=url]{grid-column:1/-1}.native-audio-tools{grid-template-columns:1fr}}
</style>
</head>
<body class="%s">
%s
<main class="stage">%s</main>
%s
</body>
</html>`, html.EscapeString(robots), html.EscapeString(title), html.EscapeString(description), html.EscapeString(previewURL), html.EscapeString(ogType), html.EscapeString(title), html.EscapeString(ogDescription), html.EscapeString(previewURL), html.EscapeString(title), html.EscapeString(ogDescription), extraMeta.String(), stylesheets, html.EscapeString(bodyClass), chrome, media.String(), scripts)
}

func imagePreviewSize(f myfiles.File) (int, int) {
	if f.ImageWidth != nil && f.ImageHeight != nil && *f.ImageWidth > 0 && *f.ImageHeight > 0 {
		return *f.ImageWidth, *f.ImageHeight
	}
	return 1200, 800
}

func captionControls() string {
	return `<div class="caption-tools"><input id="caption-url" name="subtitle" data-caption-url type="url" inputmode="url" placeholder="字幕 URL"><button data-caption-load type="button">加载</button><label class="caption-file" for="caption-file-input">文件<input id="caption-file-input" name="caption_file" data-caption-file type="file" accept=".vtt,.srt,.lrc,text/vtt,application/x-subrip,text/plain"></label></div>`
}

func previewSubtitleURL(r *http.Request) string {
	for _, key := range []string{"subtitle", "sub", "caption", "captions"} {
		if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func previewEnhancementScript(kind, subtitleURL string) string {
	switch kind {
	case "image":
		return `<script type="module">
import PhotoSwipeLightbox from "/vendor/photoswipe/photoswipe-lightbox.esm.min.js";
const lightbox = new PhotoSwipeLightbox({
  gallery: ".pswp-gallery",
  children: "a",
  pswpModule: () => import("/vendor/photoswipe/photoswipe.esm.min.js")
});
lightbox.init();
</script>`
	case "video", "audio":
		encodedURL, _ := json.Marshal(subtitleURL)
		return `<script>
const initialSubtitleURL = ` + string(encodedURL) + `;
let media = document.querySelector("[data-caption-media]");
const live = document.querySelector("[data-caption-live]");
const input = document.querySelector("[data-caption-url]");
const loadButton = document.querySelector("[data-caption-load]");
const fileInput = document.querySelector("[data-caption-file]");
const nativeAudioTools = document.querySelector("[data-native-audio-tools]");
const nativeAudioSelect = document.querySelector("[data-native-audio-select]");
const bufferNative = document.querySelector("[data-buffer-native]");
const bufferPrefetch = document.querySelector("[data-buffer-prefetch]");
const bufferPlayed = document.querySelector("[data-buffer-played]");
let cues = [];
let activeIndex = -1;
function toSeconds(raw) {
  const value = raw.trim().replace(",", ".");
  const parts = value.split(":").map(Number);
  if (parts.some(Number.isNaN)) return NaN;
  if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2];
  if (parts.length === 2) return parts[0] * 60 + parts[1];
  return parts[0];
}
function cleanCaptionText(text) {
  return text.replace(/<[^>]+>/g, "").replace(/\r/g, "").trim();
}
function parseTimedText(text) {
  const normalized = text.replace(/\ufeff/g, "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const out = [];
  if (/^\s*WEBVTT/i.test(normalized) || normalized.includes("-->")) {
    for (const block of normalized.split(/\n{2,}/)) {
      const lines = block.split("\n").filter(Boolean);
      const timeLine = lines.findIndex((line) => line.includes("-->"));
      if (timeLine < 0) continue;
      const times = lines[timeLine].split("-->");
      const start = toSeconds(times[0]);
      const end = toSeconds(times[1].split(/\s+/)[0]);
      const caption = cleanCaptionText(lines.slice(timeLine + 1).join("\n"));
      if (Number.isFinite(start) && Number.isFinite(end) && caption) out.push({ start, end, text: caption });
    }
  } else {
    for (const line of normalized.split("\n")) {
      const matches = [...line.matchAll(/\[(\d{1,2}:\d{2}(?:[.:]\d{1,3})?)\]/g)];
      if (!matches.length) continue;
      const caption = cleanCaptionText(line.replace(/\[[^\]]+\]/g, ""));
      if (!caption) continue;
      for (const match of matches) {
        const start = toSeconds(match[1]);
        if (Number.isFinite(start)) out.push({ start, end: start + 5, text: caption });
      }
    }
  }
  out.sort((a, b) => a.start - b.start);
  for (let i = 0; i < out.length; i += 1) {
    if (!Number.isFinite(out[i].end) || out[i].end <= out[i].start) {
      out[i].end = i + 1 < out.length ? out[i + 1].start : out[i].start + 5;
    }
  }
  return out;
}
function setStatus(message) {
  if (live) live.textContent = message || "";
}
function activateCues(next) {
  cues = next;
  activeIndex = -1;
  setStatus(cues.length ? "" : "未识别到字幕");
  updateCaption();
}
async function loadCaptionURL(url) {
  if (!url) return;
  setStatus("字幕加载中");
  const response = await fetch(url, { credentials: "same-origin" });
  if (!response.ok) throw new Error("HTTP " + response.status);
  activateCues(parseTimedText(await response.text()));
}
function updateCaption() {
  if (!media || !live || cues.length === 0) return;
  const now = media.currentTime;
  const index = cues.findIndex((cue) => now >= cue.start && now <= cue.end);
  if (index === activeIndex) return;
  activeIndex = index;
  live.textContent = index >= 0 ? cues[index].text : "";
}
if (input && initialSubtitleURL) input.value = initialSubtitleURL;
if (loadButton) {
  loadButton.addEventListener("click", () => {
    loadCaptionURL(input ? input.value.trim() : "").catch(() => setStatus("字幕加载失败"));
  });
}
if (fileInput) {
  fileInput.addEventListener("change", () => {
    const file = fileInput.files && fileInput.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => activateCues(parseTimedText(String(reader.result || "")));
    reader.onerror = () => setStatus("字幕加载失败");
    reader.readAsText(file);
  });
}
function audioTrackLabel(track, index) {
  return track.label || track.language || (index === 0 ? "原生音轨" : "音轨 " + (index + 1));
}
function setupNativeAudioTracks() {
  if (!media || !nativeAudioTools || !nativeAudioSelect) return;
  const tracks = media.audioTracks;
  function renderTracks() {
    nativeAudioSelect.textContent = "";
    if (!tracks || typeof tracks.length !== "number" || tracks.length === 0) {
      nativeAudioSelect.add(new Option("原生音轨", ""));
      nativeAudioSelect.disabled = true;
      return;
    }
    nativeAudioSelect.disabled = false;
    let selected = 0;
    for (let i = 0; i < tracks.length; i += 1) {
      const track = tracks[i];
      if (track.enabled) selected = i;
      nativeAudioSelect.add(new Option(audioTrackLabel(track, i), String(i)));
    }
    nativeAudioSelect.value = String(selected);
  }
  function selectTrack() {
    if (!tracks || typeof tracks.length !== "number" || tracks.length === 0) return;
    const selected = Number(nativeAudioSelect.value);
    for (let i = 0; i < tracks.length; i += 1) {
      tracks[i].enabled = i === selected;
    }
  }
  nativeAudioSelect.addEventListener("change", selectTrack);
  if (tracks && typeof tracks.addEventListener === "function") {
    tracks.addEventListener("addtrack", renderTracks);
    tracks.addEventListener("removetrack", renderTracks);
    tracks.addEventListener("change", renderTracks);
  }
  renderTracks();
}
function bindMediaControls(boundMedia, manageBuffer) {
  if (!boundMedia || boundMedia.dataset.captionBound === "1") return;
  media = boundMedia;
  media.dataset.captionBound = "1";
  media.addEventListener("timeupdate", updateCaption);
  media.addEventListener("seeked", updateCaption);
  if (!manageBuffer) return;
  let shouldResumeAfterBuffer = false;
  let lastPlayRequestAt = 0;
  let resumeTimer = 0;
  const connection = navigator.connection || navigator.mozConnection || navigator.webkitConnection;
  const prefetchSrc = media.dataset.bufferPrefetchSrc || "";
  const prefetchSize = Number(media.dataset.bufferPrefetchSize || "0");
  let prefetching = false;
  let prefetchTimer = 0;
  let nextPrefetchByte = 0;
  let confirmedPrefetchByte = 0;
  function bufferedAhead() {
    const ranges = media.buffered;
    const current = media.currentTime;
    for (let i = 0; i < ranges.length; i += 1) {
      if (ranges.start(i) <= current + 0.25 && ranges.end(i) > current) {
        return ranges.end(i) - current;
      }
    }
    return 0;
  }
  function nativeBufferedEnd() {
    let end = 0;
    for (let i = 0; i < media.buffered.length; i += 1) {
      if (media.buffered.end(i) > end) end = media.buffered.end(i);
    }
    return end;
  }
  function playbackRate() {
    return Math.max(0.25, Math.abs(media.playbackRate || 1));
  }
  function bufferPlanningRate() {
    return playbackRate();
  }
  function highRateBufferMultiplier(rate) {
    if (rate >= 2) return 1.75;
    if (rate >= 1.5) return 1.45;
    if (rate > 1) return 1.2 + (rate - 1) * 0.5;
    return 1;
  }
  function resumeThresholdSeconds() {
    const rate = bufferPlanningRate();
    let seconds = 4;
    if (connection) {
      const type = String(connection.effectiveType || "");
      if (type === "slow-2g" || type === "2g") seconds = 12;
      else if (type === "3g") seconds = 8;
      else if (type === "4g") seconds = 4;
      const downlink = Number(connection.downlink);
      if (Number.isFinite(downlink) && downlink > 0) {
        if (downlink < 0.8) seconds = Math.max(seconds, 12);
        else if (downlink < 1.5) seconds = Math.max(seconds, 8);
        else if (downlink < 3) seconds = Math.max(seconds, 5);
      }
      const rtt = Number(connection.rtt);
      if (Number.isFinite(rtt) && rtt > 600) seconds += 2;
    }
    if (rate > 1) {
      seconds = Math.max(seconds, 6 + (rate - 1) * 4);
    }
    return Math.min(45, Math.max(2.5, seconds * rate * highRateBufferMultiplier(rate)));
  }
  function canResumePlayback() {
    if (!shouldResumeAfterBuffer || media.ended || media.seeking) return false;
    const threshold = resumeThresholdSeconds();
    const rate = bufferPlanningRate();
    const ahead = bufferedAhead();
    if (media.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && ahead >= Math.max(0.35, 0.75 * rate)) {
      return true;
    }
    if (prefetchedAhead() >= Math.max(2.5, 2 * rate)) {
      return true;
    }
    return ahead >= threshold;
  }
  function prefetchedAhead() {
    if (!prefetchSize || !Number.isFinite(media.duration) || media.duration <= 0) return 0;
    const bytesPerSecond = prefetchSize / media.duration;
    if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return 0;
    const currentByte = Math.max(0, Math.floor(media.currentTime * bytesPerSecond));
    return Math.max(0, (confirmedPrefetchByte - currentByte) / bytesPerSecond);
  }
  function confirmedPrefetchEndTime() {
    if (!prefetchSize || !Number.isFinite(media.duration) || media.duration <= 0) return 0;
    return Math.min(media.duration, (confirmedPrefetchByte / prefetchSize) * media.duration);
  }
  function setMeterWidth(element, seconds) {
    if (!element || !Number.isFinite(media.duration) || media.duration <= 0) return;
    element.style.width = Math.max(0, Math.min(100, (seconds / media.duration) * 100)) + "%";
  }
  function updateBufferMeter() {
    setMeterWidth(bufferNative, nativeBufferedEnd());
    setMeterWidth(bufferPrefetch, Math.max(nativeBufferedEnd(), confirmedPrefetchEndTime()));
    setMeterWidth(bufferPlayed, media.currentTime || 0);
  }
  function prefetchTargetSeconds() {
    const rate = bufferPlanningRate();
    let seconds = rate > 1 ? 18 * rate : 10;
    if (connection) {
      const downlink = Number(connection.downlink);
      if (Number.isFinite(downlink) && downlink > 0 && downlink < 1.5) seconds = Math.min(seconds, 24);
    }
    return Math.min(90, seconds);
  }
  function shouldPrefetchAhead() {
    if (!prefetchSrc || !prefetchSize || !Number.isFinite(media.duration) || media.duration <= 0 || media.ended) return false;
    const rate = bufferPlanningRate();
    return rate > 1.01 || shouldResumeAfterBuffer || bufferedAhead() < Math.min(8, resumeThresholdSeconds());
  }
  function schedulePrefetchAhead(delay) {
    window.clearTimeout(prefetchTimer);
    if (!shouldPrefetchAhead()) return;
    prefetchTimer = window.setTimeout(prefetchAhead, delay || 0);
  }
  async function prefetchAhead() {
    if (prefetching || !shouldPrefetchAhead()) return;
    const bytesPerSecond = prefetchSize / media.duration;
    if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return;
    const currentByte = Math.max(0, Math.floor(media.currentTime * bytesPerSecond));
    if (nextPrefetchByte < currentByte || media.seeking) nextPrefetchByte = currentByte;
    const start = Math.max(nextPrefetchByte, Math.floor((media.currentTime + Math.min(bufferedAhead() + 2, 18)) * bytesPerSecond));
    const target = Math.min(prefetchSize - 1, Math.floor((media.currentTime + prefetchTargetSeconds()) * bytesPerSecond));
    if (start >= target) return;
    const chunkSeconds = bufferPlanningRate() >= 2 ? 18 : bufferPlanningRate() >= 1.5 ? 12 : 8;
    const chunkSize = Math.max(384 * 1024, Math.ceil(bytesPerSecond * chunkSeconds * bufferPlanningRate()));
    const end = Math.min(target, start + chunkSize - 1);
    prefetching = true;
    try {
      const response = await fetch(prefetchSrc, {
        headers: { Range: "bytes=" + start + "-" + end },
        credentials: "same-origin",
        cache: "force-cache"
      });
      if (response.ok || response.status === 206) {
        await response.arrayBuffer();
        nextPrefetchByte = end + 1;
        confirmedPrefetchByte = Math.max(confirmedPrefetchByte, end + 1);
        updateBufferMeter();
      }
    } catch (_) {
      nextPrefetchByte = Math.min(prefetchSize - 1, end + 1);
    } finally {
      prefetching = false;
      if (!media.paused || shouldResumeAfterBuffer) schedulePrefetchAhead(650);
    }
  }
  function tryResumePlayback() {
    window.clearTimeout(resumeTimer);
    if (!canResumePlayback()) return;
    const playResult = media.play();
    if (playResult && typeof playResult.catch === "function") {
      playResult.catch(() => {});
    }
  }
  function scheduleResumeCheck() {
    tryResumePlayback();
    if (shouldResumeAfterBuffer && media.paused && !media.ended) {
      resumeTimer = window.setTimeout(scheduleResumeCheck, 500);
    }
  }
  media.addEventListener("play", () => {
    lastPlayRequestAt = Date.now();
    media.preload = "auto";
    schedulePrefetchAhead(0);
    updateBufferMeter();
    shouldResumeAfterBuffer = false;
  });
  media.addEventListener("pause", () => {
    if (Date.now() - lastPlayRequestAt > 900 && !media.seeking && !media.ended && bufferedAhead() > 0.5) {
      shouldResumeAfterBuffer = false;
    } else if (shouldResumeAfterBuffer && !media.ended) {
      scheduleResumeCheck();
    }
  });
  media.addEventListener("waiting", () => {
    shouldResumeAfterBuffer = true;
    schedulePrefetchAhead(0);
    scheduleResumeCheck();
  });
  media.addEventListener("stalled", () => {
    if (!media.paused || shouldResumeAfterBuffer) {
      shouldResumeAfterBuffer = true;
      schedulePrefetchAhead(0);
      scheduleResumeCheck();
    }
  });
  media.addEventListener("progress", () => {
    updateBufferMeter();
    schedulePrefetchAhead(250);
    scheduleResumeCheck();
  });
  media.addEventListener("canplay", () => {
    updateBufferMeter();
    schedulePrefetchAhead(250);
    scheduleResumeCheck();
  });
  media.addEventListener("canplaythrough", scheduleResumeCheck);
  media.addEventListener("timeupdate", updateBufferMeter);
  media.addEventListener("seeked", () => {
    nextPrefetchByte = 0;
    confirmedPrefetchByte = 0;
    updateBufferMeter();
    schedulePrefetchAhead(0);
  });
  media.addEventListener("ratechange", () => {
    media.preload = "auto";
    schedulePrefetchAhead(0);
    scheduleResumeCheck();
  });
  updateBufferMeter();
  if (media.readyState >= HTMLMediaElement.HAVE_METADATA) schedulePrefetchAhead(300);
  else media.addEventListener("loadedmetadata", () => {
    updateBufferMeter();
    schedulePrefetchAhead(300);
  }, { once: true });
}
bindMediaControls(media, true);
setupNativeAudioTracks();
if (initialSubtitleURL) loadCaptionURL(initialSubtitleURL).catch(() => setStatus("字幕加载失败"));
</script>`
	default:
		return ""
	}
}

func publicOGImagePath(id, name string) string {
	return "/og/" + strings.TrimPrefix(publicFilePath(id, name), "/files/") + ".jpg"
}

func (a *App) handlePublicOGImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/og/")
	rest = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(rest, ".svg"), ".png"), ".jpg")
	id := publicFileID(rest)
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic || f.Status != "active" || !isPreviewableMedia(f.MIME, f.OriginalName) {
		http.NotFound(w, r)
		return
	}
	if mediaKind(f.MIME, f.OriginalName) == "video" {
		poster, err := a.videoPosterPath(r.Context(), f)
		if err == nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
			http.ServeFile(w, r, poster)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	_ = png.Encode(w, blankOGImage())
}

func (a *App) videoPosterPath(ctx context.Context, f myfiles.File) (string, error) {
	cacheDir := filepath.Join(a.cfg.App.DataDir, "cache", "posters")
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return "", err
	}
	posterPath := filepath.Join(cacheDir, f.ID+".jpg")
	if info, err := os.Stat(posterPath); err == nil && info.Size() > 0 && posterLooksUsable(posterPath) {
		return posterPath, nil
	}
	_ = os.Remove(posterPath)
	source := a.videoPosterSource(f)
	if source == "" {
		return "", fmt.Errorf("missing poster source")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if attached, err := a.extractAttachedPoster(ctx, cacheDir, f.ID, source); err == nil && posterLooksUsable(attached) {
		if err := os.Rename(attached, posterPath); err != nil {
			_ = os.Remove(attached)
			return "", err
		}
		return posterPath, nil
	}
	if frame, err := extractBestPosterFrame(ctx, cacheDir, f.ID, source); err == nil {
		if err := os.Rename(frame, posterPath); err != nil {
			_ = os.Remove(frame)
			return "", err
		}
		return posterPath, nil
	}
	return "", fmt.Errorf("no usable poster")
}

func (a *App) downloadStoredThumbnail(ctx context.Context, cacheDir string, f myfiles.File) (string, error) {
	if f.StorageProvider != "tgbots" || strings.TrimSpace(f.ThumbnailFileID) == "" {
		return "", fmt.Errorf("missing thumbnail file id")
	}
	cfg := a.snapshotConfig()
	url := storage.FetchURL(cfg.Storage.UploadURL, cfg.Storage.APIKey, f.ThumbnailFileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := storage.NewTGBotsHTTPClient(cfg.Storage).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("thumbnail fetch failed: HTTP %d", resp.StatusCode)
	}
	tmpPath := filepath.Join(cacheDir, f.ID+"."+ids.New("thumb")+".jpg")
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 16<<20)); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func (a *App) extractAttachedPoster(ctx context.Context, cacheDir, id, source string) (string, error) {
	tmpPath := filepath.Join(cacheDir, id+"."+ids.New("cover")+".jpg")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", source, "-map", "0:v:m:attached_pic", "-frames:v", "1", tmpPath)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func extractBestPosterFrame(ctx context.Context, cacheDir, id, source string) (string, error) {
	var bestPath string
	bestScore := -1.0
	for _, ts := range posterCandidateTimes(ctx, source) {
		tmpPath := filepath.Join(cacheDir, id+"."+ids.New("frame")+".jpg")
		if err := extractPosterFrame(ctx, source, tmpPath, ts); err != nil {
			_ = os.Remove(tmpPath)
			continue
		}
		score, ok := posterQuality(tmpPath)
		if !ok {
			_ = os.Remove(tmpPath)
			continue
		}
		if score > bestScore {
			if bestPath != "" {
				_ = os.Remove(bestPath)
			}
			bestPath = tmpPath
			bestScore = score
			continue
		}
		_ = os.Remove(tmpPath)
	}
	if bestPath == "" {
		return "", fmt.Errorf("no usable poster frame")
	}
	return bestPath, nil
}

func posterCandidateTimes(ctx context.Context, source string) []string {
	seconds := []float64{3, 8, 15, 30, 60}
	if duration := probeVideoDuration(ctx, source); duration > 0 {
		for _, pct := range []float64{0.08, 0.15, 0.25, 0.38, 0.5} {
			seconds = append(seconds, duration*pct)
		}
		seconds = append(seconds, duration-2)
	}
	seen := map[int]bool{}
	out := make([]string, 0, len(seconds))
	for _, sec := range seconds {
		if sec < 0.5 {
			continue
		}
		key := int(sec * 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, fmt.Sprintf("%.3f", sec))
	}
	if len(out) == 0 {
		return []string{"1.000"}
	}
	return out
}

func probeVideoDuration(ctx context.Context, source string) float64 {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", source)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

func extractPosterFrame(ctx context.Context, source, tmpPath, ts string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-ss", ts, "-i", source, "-frames:v", "1", "-vf", "scale=w='min(1280,iw)':h=-2", "-q:v", "3", tmpPath)
	return cmd.Run()
}

func posterLooksUsable(path string) bool {
	_, ok := posterQuality(path)
	return ok
}

func posterQuality(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return 0, false
	}
	b := img.Bounds()
	totalPixels := b.Dx() * b.Dy()
	if totalPixels <= 0 {
		return 0, false
	}
	step := totalPixels / 50000
	if step < 1 {
		step = 1
	}
	var count int
	var sum, sumSq float64
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if i%step != 0 {
				i++
				continue
			}
			r, g, bl, _ := img.At(x, y).RGBA()
			luma := 0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(bl>>8)
			sum += luma
			sumSq += luma * luma
			count++
			i++
		}
	}
	if count == 0 {
		return 0, false
	}
	mean := sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	if mean < 12 || mean > 244 || variance < 80 {
		return variance + mean, false
	}
	return variance + mean, true
}

func (a *App) videoPosterSource(f myfiles.File) string {
	if f.StorageProvider == "local" && f.StorageURL != "" {
		return f.StorageURL
	}
	if f.StorageProvider == "tgbots" && f.StorageFileID != "" {
		cfg := a.snapshotConfig()
		if storage.ValidBotToken(cfg.Storage.APIKey) {
			return storage.FetchURL(cfg.Storage.UploadURL, cfg.Storage.APIKey, f.StorageFileID)
		}
	}
	return strings.TrimSpace(f.StorageURL)
}

func blankOGImage() image.Image {
	const width, height = 1200, 630
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
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
	if r.Header.Get("Range") != "" {
		req.Header.Set("Range", r.Header.Get("Range"))
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
	w.Header().Set("Content-Type", effectiveFileMIME(f))
	w.Header().Set("Content-Disposition", contentDisposition(r, f))
	copyResponseHeader(w, resp, "Accept-Ranges")
	copyResponseHeader(w, resp, "Content-Length")
	copyResponseHeader(w, resp, "Content-Range")
	copyResponseHeader(w, resp, "ETag")
	copyResponseHeader(w, resp, "Last-Modified")
	setStoredFileCacheHeaders(w, f, f.IsPublic)
	status := resp.StatusCode
	if status < 200 || status >= 400 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	buf := make([]byte, 1024*1024)
	_, _ = io.CopyBuffer(w, resp.Body, buf)
}

func copyResponseHeader(w http.ResponseWriter, resp *http.Response, name string) {
	if value := resp.Header.Get(name); value != "" {
		w.Header().Set(name, value)
	}
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
		"id": f.ID, "name": f.OriginalName, "mime": effectiveFileMIME(f), "size": f.Size, "sha256": f.SHA256,
		"imageWidth": f.ImageWidth, "imageHeight": f.ImageHeight, "requireConfirm": f.RequireConfirm, "url": publicFilePath(f.ID, f.OriginalName),
		"previewUrl": publicFilePath(f.ID, f.OriginalName), "embedUrl": publicFilePath(f.ID, f.OriginalName),
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
	http.SetCookie(w, &http.Cookie{Name: "myfiles_file_confirm_" + id, Value: "1", Path: "/files/" + id, MaxAge: 3600, Secure: a.cfg.Security.CookieSecure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]any{"ok": true, "url": publicFilePath(id, f.OriginalName)})
}

func (a *App) handlePublicFileDownloadConfirm(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic || f.Status != "active" {
		http.NotFound(w, r)
		return
	}
	if isPreviewableMedia(f.MIME, f.OriginalName) {
		http.Redirect(w, r, publicFilePath(f.ID, f.OriginalName), http.StatusSeeOther)
		return
	}
	a.confirmDownload(w, r, downloadConfirmCookieName("file", f.ID), "/files")
	http.Redirect(w, r, publicFilePath(f.ID, f.OriginalName)+"/download", http.StatusSeeOther)
}

func (a *App) handlePublicFileDownload(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	f, err := myfiles.GetFile(a.db, id, false)
	if err != nil || !f.IsPublic || f.Status != "active" {
		http.NotFound(w, r)
		return
	}
	if isPreviewableMedia(f.MIME, f.OriginalName) {
		http.Redirect(w, r, publicFilePath(f.ID, f.OriginalName), http.StatusSeeOther)
		return
	}
	name := downloadConfirmCookieName("file", f.ID)
	if !a.hasDownloadConfirmation(r, name) {
		http.Redirect(w, r, publicFilePath(f.ID, f.OriginalName), http.StatusSeeOther)
		return
	}
	a.clearDownloadConfirmation(w, r, name, "/files")
	a.serveStoredFile(w, r, f, true)
}

func (a *App) confirmDownload(w http.ResponseWriter, r *http.Request, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "1", Path: path, MaxAge: 60, Secure: a.cfg.Security.CookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (a *App) hasDownloadConfirmation(r *http.Request, name string) bool {
	c, err := r.Cookie(name)
	return err == nil && c.Value == "1"
}

func (a *App) clearDownloadConfirmation(w http.ResponseWriter, r *http.Request, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, Secure: a.cfg.Security.CookieSecure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func downloadConfirmCookieName(parts ...string) string {
	return "myfiles_download_confirm_" + strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(strings.Join(parts, "_"))
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
		return "/files/" + id
	}
	return "/files/" + id + ext
}

func publicRawFilePath(id, name string) string {
	return "/files/raw/" + strings.TrimPrefix(publicFilePath(id, name), "/files/")
}

func publicFaststartFilePath(id, name string) string {
	return "/files/faststart/" + strings.TrimPrefix(publicFilePath(id, name), "/files/")
}

func canonicalPublicURL(f myfiles.File) string {
	return publicFilePath(f.ID, f.OriginalName)
}

func redirectNoStore(w http.ResponseWriter, r *http.Request, target string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
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
		http.Redirect(w, r, "/?code="+url.QueryEscape(code), http.StatusFound)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/uploads"), "/")
	if id != "" {
		http.Redirect(w, r, "/?upload="+url.QueryEscape(id), http.StatusFound)
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
