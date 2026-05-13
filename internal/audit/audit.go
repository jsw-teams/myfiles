package audit

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jsw-teams/myfiles/internal/ids"
)

type Actor struct {
	AccountUserID string
	Role          string
}

func Write(conn *sql.DB, r *http.Request, actor Actor, action, targetType, targetID string, detail any) {
	raw, _ := json.Marshal(detail)
	ip, ua := "", ""
	if r != nil {
		ua = r.UserAgent()
		if v := r.Header.Get("CF-Connecting-IP"); v != "" {
			ip = v
		} else if v := r.Header.Get("X-Forwarded-For"); v != "" {
			ip = v
		} else {
			ip = r.RemoteAddr
		}
	}
	_, _ = conn.Exec(`INSERT INTO audit_logs
		(id, actor_account_user_id, actor_role, action, target_type, target_id, detail_json, ip, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.New("aud"), actor.AccountUserID, actor.Role, action, targetType, targetID, string(raw), ip, ua, time.Now().UTC().Format(time.RFC3339))
}
