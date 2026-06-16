package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jsw-teams/myfiles/internal/account"
	"github.com/jsw-teams/myfiles/internal/audit"
	"github.com/jsw-teams/myfiles/internal/ids"
)

const (
	passwordHashVersion = "pbkdf2-sha256"
	passwordIterations  = 210000
	loginLockThreshold  = 5
	loginLockDuration   = 10 * time.Minute
)

type localUser struct {
	ID               string
	Email            string
	DisplayName      string
	Role             string
	PasswordHash     string
	Disabled         bool
	FailedLoginCount int
	LockedUntil      string
	CreatedAt        string
	UpdatedAt        string
	LastLoginAt      string
}

type authOptions struct {
	Initialized       bool `json:"initialized"`
	AllowRegistration bool `json:"allowRegistration"`
	SSOConfigured     bool `json:"ssoConfigured"`
	SSOEnabled        bool `json:"ssoEnabled"`
}

func (a *App) initialized() bool {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (a *App) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	opts := a.currentAuthOptions()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "initialized": opts.Initialized, "allowRegistration": opts.AllowRegistration, "ssoEnabled": opts.SSOEnabled})
}

func (a *App) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if a.initialized() {
		writeError(w, http.StatusConflict, "already_initialized", "系统已初始化", nil)
		return
	}
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体格式错误", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" {
		displayName = email
	}
	if !strings.Contains(email, "@") || len(body.Password) < 10 {
		writeError(w, http.StatusBadRequest, "bad_setup", "邮箱或密码不符合要求", nil)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", "密码处理失败", nil)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := ids.New("usr")
	_, err = a.db.Exec(`INSERT INTO users
		(id, email, display_name, role, password_hash, disabled, failed_login_count, created_at, updated_at)
		VALUES (?, ?, ?, 'system_admin', ?, 0, 0, ?, ?)`, id, email, displayName, hash, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "创建管理员失败", nil)
		return
	}
	user := account.User{ID: id, Email: email, DisplayName: displayName, Role: "system_admin", UserType: "local", Capabilities: map[string]any{}}
	if err := a.createSession(w, user); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "会话创建失败", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: id, Role: "system_admin"}, "setup.init", "user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": user})
}

func (a *App) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.initialized() {
		writeError(w, http.StatusPreconditionRequired, "setup_required", "系统尚未初始化", map[string]any{"setupPath": "/setup"})
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体格式错误", nil)
		return
	}
	user, err := a.localUserByEmail(strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil || user.Disabled {
		writeError(w, http.StatusUnauthorized, "bad_credentials", "邮箱或密码错误", nil)
		return
	}
	if locked(user.LockedUntil) {
		writeError(w, http.StatusTooManyRequests, "account_locked", "登录失败次数过多，请稍后再试", nil)
		return
	}
	if !verifyPassword(user.PasswordHash, body.Password) {
		a.recordLoginFailure(user)
		writeError(w, http.StatusUnauthorized, "bad_credentials", "邮箱或密码错误", nil)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = a.db.Exec(`UPDATE users SET failed_login_count=0, locked_until=NULL, last_login_at=?, updated_at=? WHERE id=?`, now, now, user.ID)
	sessionUser := account.User{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role, UserType: "local", Capabilities: map[string]any{}}
	if err := a.createSession(w, sessionUser); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "会话创建失败", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: user.ID, Role: user.Role}, "auth.login", "local_user", user.ID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": sessionUser})
}

func (a *App) handleLocalRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	opts := a.currentAuthOptions()
	if !opts.Initialized {
		writeError(w, http.StatusPreconditionRequired, "setup_required", "系统尚未初始化", map[string]any{"setupPath": "/setup"})
		return
	}
	if !opts.AllowRegistration {
		writeError(w, http.StatusForbidden, "registration_disabled", "当前站点未开放自主注册", nil)
		return
	}
	var body struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体格式错误", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" {
		displayName = email
	}
	if !strings.Contains(email, "@") || len(body.Password) < 10 {
		writeError(w, http.StatusBadRequest, "bad_register", "邮箱或密码不符合要求", nil)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", "密码处理失败", nil)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := ids.New("usr")
	_, err = a.db.Exec(`INSERT INTO users
		(id, email, display_name, role, password_hash, disabled, failed_login_count, created_at, updated_at)
		VALUES (?, ?, ?, 'user', ?, 0, 0, ?, ?)`, id, email, displayName, hash, now, now)
	if err != nil {
		writeError(w, http.StatusConflict, "user_exists", "该邮箱已注册", nil)
		return
	}
	user := account.User{ID: id, Email: email, DisplayName: displayName, Role: "user", UserType: "local", Capabilities: map[string]any{}}
	if err := a.createSession(w, user); err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "会话创建失败", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: id, Role: "user"}, "auth.register", "local_user", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": user})
}

func (a *App) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	a.handleAccountMe(w, r)
}

func (a *App) handleAuthOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	opts := a.currentAuthOptions()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"initialized":       opts.Initialized,
		"allowRegistration": opts.AllowRegistration,
		"ssoConfigured":     opts.SSOConfigured,
		"ssoEnabled":        opts.SSOEnabled,
		"loginPath":         "/login",
		"registerPath":      "/register",
		"ssoStartPath":      "/auth/account/start?popup=1",
	})
}

func (a *App) localUserByID(id string) (localUser, error) {
	var u localUser
	var disabled int
	var locked, last sql.NullString
	err := a.db.QueryRow(`SELECT id, email, display_name, role, COALESCE(password_hash,''), disabled,
		failed_login_count, locked_until, created_at, updated_at, last_login_at
		FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.PasswordHash, &disabled, &u.FailedLoginCount, &locked, &u.CreatedAt, &u.UpdatedAt, &last)
	u.Disabled = disabled != 0
	u.LockedUntil = locked.String
	u.LastLoginAt = last.String
	return u, err
}

func (a *App) localUserByIdentity(provider, providerUserID string) (localUser, error) {
	var userID string
	if err := a.db.QueryRow(`SELECT user_id FROM user_identities WHERE provider=? AND provider_user_id=?`, provider, providerUserID).Scan(&userID); err != nil {
		return localUser{}, err
	}
	return a.localUserByID(userID)
}

func (a *App) upsertSSOUser(provider string, remote account.User) (account.User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if u, err := a.localUserByIdentity(provider, remote.ID); err == nil {
		_, _ = a.db.Exec(`UPDATE user_identities SET email=?, display_name=?, updated_at=? WHERE provider=? AND provider_user_id=?`,
			remote.Email, remote.DisplayName, now, provider, remote.ID)
		return account.User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Role: u.Role, UserType: "local+sso", Capabilities: map[string]any{}}, nil
	}
	email := strings.ToLower(strings.TrimSpace(remote.Email))
	if email == "" {
		email = strings.ToLower(remote.ID) + "@account-system.local"
	}
	displayName := strings.TrimSpace(remote.DisplayName)
	if displayName == "" {
		displayName = email
	}
	var local localUser
	if existing, err := a.localUserByEmail(email); err == nil {
		local = existing
	} else {
		id := ids.New("usr")
		_, err := a.db.Exec(`INSERT INTO users
			(id, email, display_name, role, password_hash, disabled, failed_login_count, created_at, updated_at)
			VALUES (?, ?, ?, 'user', '', 0, 0, ?, ?)`, id, email, displayName, now, now)
		if err != nil {
			return account.User{}, err
		}
		local = localUser{ID: id, Email: email, DisplayName: displayName, Role: "user"}
	}
	_, err := a.db.Exec(`INSERT OR REPLACE INTO user_identities
		(id, user_id, provider, provider_user_id, email, display_name, created_at, updated_at)
		VALUES (COALESCE((SELECT id FROM user_identities WHERE provider=? AND provider_user_id=?), ?), ?, ?, ?, ?, ?, ?, ?)`,
		provider, remote.ID, ids.New("uid"), local.ID, provider, remote.ID, remote.Email, remote.DisplayName, now, now)
	if err != nil {
		return account.User{}, err
	}
	return account.User{ID: local.ID, Email: local.Email, DisplayName: local.DisplayName, Role: local.Role, UserType: "local+sso", Capabilities: map[string]any{}}, nil
}

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.UsersRead })
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := a.db.Query(`SELECT id, email, display_name, role, disabled, created_at, updated_at, COALESCE(last_login_at,'')
			FROM users ORDER BY created_at DESC LIMIT 500`)
		if err != nil {
			writeError(w, 500, "db_error", "读取用户失败", nil)
			return
		}
		defer rows.Close()
		users := []map[string]any{}
		for rows.Next() {
			var id, email, displayName, role, created, updated, last string
			var disabled int
			if err := rows.Scan(&id, &email, &displayName, &role, &disabled, &created, &updated, &last); err != nil {
				writeError(w, 500, "db_error", "读取用户失败", nil)
				return
			}
			users = append(users, map[string]any{"id": id, "email": email, "displayName": displayName, "role": role, "disabled": disabled != 0, "createdAt": created, "updatedAt": updated, "lastLoginAt": last})
		}
		writeJSON(w, 200, map[string]any{"ok": true, "users": users})
	case http.MethodPost:
		if !s.Permissions.UsersWrite {
			writeError(w, 403, "forbidden", "无权创建用户", nil)
			return
		}
		var body struct {
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
			Password    string `json:"password"`
			Role        string `json:"role"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeError(w, 400, "bad_json", "请求体格式错误", nil)
			return
		}
		user, err := a.createLocalUser(strings.ToLower(strings.TrimSpace(body.Email)), strings.TrimSpace(body.DisplayName), body.Password, normalizeUserRole(body.Role))
		if err != nil {
			writeError(w, 400, "user_create_failed", err.Error(), nil)
			return
		}
		audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "admin.user.create", "local_user", user.ID, map[string]any{"email": user.Email, "role": user.Role})
		writeJSON(w, 200, map[string]any{"ok": true, "user": user})
	default:
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
	}
}

func (a *App) handleAdminUserAPI(w http.ResponseWriter, r *http.Request) {
	s, ok := a.requireAdmin(w, r, func(p MyfilesPermissions) bool { return p.UsersWrite })
	if !ok {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"), "/")
	if id == "" {
		writeError(w, 404, "not_found", "not found", nil)
		return
	}
	if r.Method != http.MethodPatch {
		writeError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, 400, "bad_json", "请求体格式错误", nil)
		return
	}
	sets := []string{}
	args := []any{}
	if v, ok := body["displayName"]; ok {
		sets = append(sets, "display_name=?")
		args = append(args, strings.TrimSpace(stringValue(v)))
	}
	if v, ok := body["role"]; ok {
		sets = append(sets, "role=?")
		args = append(args, normalizeUserRole(stringValue(v)))
	}
	if v, ok := body["disabled"]; ok {
		sets = append(sets, "disabled=?")
		if boolValue(v) {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if v, ok := body["password"]; ok {
		password := stringValue(v)
		if password != "" {
			if len(password) < 10 {
				writeError(w, 400, "bad_password", "密码至少需要 10 个字符", nil)
				return
			}
			hash, err := hashPassword(password)
			if err != nil {
				writeError(w, 500, "hash_failed", "密码处理失败", nil)
				return
			}
			sets = append(sets, "password_hash=?")
			args = append(args, hash)
		}
	}
	if len(sets) == 0 {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UTC().Format(time.RFC3339), id)
	res, err := a.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	if err != nil {
		writeError(w, 500, "db_error", "更新用户失败", nil)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, 404, "not_found", "用户不存在", nil)
		return
	}
	audit.Write(a.db, r, audit.Actor{AccountUserID: s.User.ID, Role: s.User.Role}, "admin.user.patch", "local_user", id, body)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) createLocalUser(email, displayName, password, role string) (account.User, error) {
	if displayName == "" {
		displayName = email
	}
	if !strings.Contains(email, "@") || len(password) < 10 {
		return account.User{}, fmt.Errorf("邮箱或密码不符合要求")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return account.User{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := ids.New("usr")
	_, err = a.db.Exec(`INSERT INTO users
		(id, email, display_name, role, password_hash, disabled, failed_login_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)`, id, email, displayName, role, hash, now, now)
	if err != nil {
		return account.User{}, fmt.Errorf("该邮箱已存在")
	}
	return account.User{ID: id, Email: email, DisplayName: displayName, Role: role, UserType: "local", Capabilities: map[string]any{}}, nil
}

func normalizeUserRole(role string) string {
	switch strings.TrimSpace(role) {
	case "system_admin", "operator", "auditor", "user":
		return strings.TrimSpace(role)
	default:
		return "user"
	}
}

func (a *App) currentAuthOptions() authOptions {
	cfg := a.snapshotConfig()
	configured := strings.TrimSpace(cfg.Account.LoginURL) != "" &&
		strings.TrimSpace(cfg.Account.ClientID) != "" &&
		strings.TrimSpace(cfg.Account.RedirectURI) != ""
	opts := authOptions{Initialized: a.initialized(), SSOConfigured: configured, SSOEnabled: configured}
	if v, ok := a.settingBool("auth.allowRegistration"); ok {
		opts.AllowRegistration = v
	}
	if v, ok := a.settingBool("auth.ssoEnabled"); ok {
		opts.SSOEnabled = configured && v
	}
	return opts
}

func (a *App) localUserByEmail(email string) (localUser, error) {
	var u localUser
	var disabled int
	var locked, last sql.NullString
	err := a.db.QueryRow(`SELECT id, email, display_name, role, COALESCE(password_hash,''), disabled,
		failed_login_count, locked_until, created_at, updated_at, last_login_at
		FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.PasswordHash, &disabled, &u.FailedLoginCount, &locked, &u.CreatedAt, &u.UpdatedAt, &last)
	u.Disabled = disabled != 0
	u.LockedUntil = locked.String
	u.LastLoginAt = last.String
	return u, err
}

func (a *App) recordLoginFailure(u localUser) {
	count := u.FailedLoginCount + 1
	now := time.Now().UTC()
	lockedUntil := any(nil)
	if count >= loginLockThreshold {
		lockedUntil = now.Add(loginLockDuration).Format(time.RFC3339)
	}
	_, _ = a.db.Exec(`UPDATE users SET failed_login_count=?, locked_until=?, updated_at=? WHERE id=?`,
		count, lockedUntil, now.Format(time.RFC3339), u.ID)
}

func locked(value string) bool {
	if value == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, value)
	return err == nil && time.Now().UTC().Before(t)
}

func hashPassword(password string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt[:], passwordIterations, 32)
	return fmt.Sprintf("%s$%d$%s$%s", passwordHashVersion, passwordIterations, base64.RawStdEncoding.EncodeToString(salt[:]), hex.EncodeToString(key)), nil
}

func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != passwordHashVersion {
		return false
	}
	var iter int
	if _, err := fmt.Sscanf(parts[1], "%d", &iter); err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hLen := 32
	numBlocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, numBlocks*hLen)
	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
