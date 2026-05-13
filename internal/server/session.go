package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jsw-teams/myfiles/internal/account"
	"github.com/jsw-teams/myfiles/internal/ids"
)

type Session struct {
	ID          string
	User        account.User
	Permissions MyfilesPermissions
	ExpiresAt   time.Time
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (a *App) createSession(w http.ResponseWriter, user account.User) error {
	now := time.Now().UTC()
	raw := ids.New("ses")
	caps, _ := json.Marshal(user.Capabilities)
	ttl := time.Duration(a.cfg.Security.SessionTTLHours) * time.Hour
	expires := now.Add(ttl)

	_, err := a.db.Exec(`INSERT INTO account_sessions
		(id, session_hash, account_user_id, email, display_name, role, user_type, capabilities_json, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.New("sess"), hashToken(raw), user.ID, user.Email, user.DisplayName, user.Role, user.UserType, string(caps),
		expires.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.Security.SessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   a.cfg.Security.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *App) readSession(r *http.Request) (*Session, error) {
	c, err := r.Cookie(a.cfg.Security.SessionCookieName)
	if err != nil || c.Value == "" {
		return nil, sql.ErrNoRows
	}
	var s Session
	var capsRaw string
	var expRaw string
	err = a.db.QueryRow(`SELECT id, account_user_id, email, display_name, role, user_type, capabilities_json, expires_at
		FROM account_sessions WHERE session_hash=?`, hashToken(c.Value)).
		Scan(&s.ID, &s.User.ID, &s.User.Email, &s.User.DisplayName, &s.User.Role, &s.User.UserType, &capsRaw, &expRaw)
	if err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339, expRaw)
	if err != nil || time.Now().UTC().After(exp) {
		return nil, sql.ErrNoRows
	}
	_ = json.Unmarshal([]byte(capsRaw), &s.User.Capabilities)
	if s.User.Capabilities == nil {
		s.User.Capabilities = map[string]any{}
	}
	s.ExpiresAt = exp
	s.Permissions = derivePermissions(s.User.Role, s.User.UserType, s.User.Capabilities)
	_, _ = a.db.Exec(`UPDATE account_sessions SET last_seen_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), s.ID)
	return &s, nil
}

func (a *App) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(a.cfg.Security.SessionCookieName); err == nil {
		_, _ = a.db.Exec(`DELETE FROM account_sessions WHERE session_hash=?`, hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.Security.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cfg.Security.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
