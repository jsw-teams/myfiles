package db

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
)

func Migrate(conn *sql.DB) error {
	if _, err := conn.Exec(schema); err != nil {
		return err
	}
	if err := ensurePickupColumns(conn); err != nil {
		return err
	}
	if err := ensureFileColumns(conn); err != nil {
		return err
	}
	if err := ensureAccountTables(conn); err != nil {
		return err
	}
	if err := normalizeEmptyPickupCodes(conn); err != nil {
		return err
	}
	return migratePublicFileURLs(conn)
}

func ensurePickupColumns(conn *sql.DB) error {
	rows, err := conn.Query(`PRAGMA table_info(upload_batches)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["pickup_code"] {
		if _, err := conn.Exec(`ALTER TABLE upload_batches ADD COLUMN pickup_code TEXT`); err != nil {
			return err
		}
	}
	if !cols["pickup_expires_at"] {
		if _, err := conn.Exec(`ALTER TABLE upload_batches ADD COLUMN pickup_expires_at TEXT`); err != nil {
			return err
		}
	}
	_, err = conn.Exec(pickupShareSchema)
	return err
}

func ensureFileColumns(conn *sql.DB) error {
	rows, err := conn.Query(`PRAGMA table_info(files)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["thumbnail_file_id"] {
		if _, err := conn.Exec(`ALTER TABLE files ADD COLUMN thumbnail_file_id TEXT`); err != nil {
			return err
		}
	}
	return nil
}

func ensureAccountTables(conn *sql.DB) error {
	_, err := conn.Exec(accountSchema)
	return err
}

func normalizeEmptyPickupCodes(conn *sql.DB) error {
	_, err := conn.Exec(`UPDATE upload_batches SET pickup_code=NULL, pickup_expires_at=NULL WHERE pickup_code=''`)
	return err
}

func migratePublicFileURLs(conn *sql.DB) error {
	rows, err := conn.Query(`SELECT id, original_name, public_url FROM files`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type fileURL struct {
		id        string
		name      string
		publicURL string
	}
	var files []fileURL
	for rows.Next() {
		var f fileURL
		if err := rows.Scan(&f.id, &f.name, &f.publicURL); err != nil {
			return err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range files {
		next := publicURLOrigin(f.publicURL) + canonicalFilePath(f.id, f.name)
		if next == f.publicURL {
			continue
		}
		if _, err := conn.Exec(`UPDATE files SET public_url=? WHERE id=?`, next, f.id); err != nil {
			return err
		}
	}
	return nil
}

func canonicalFilePath(id, name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" || len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return "/files/" + id
	}
	return "/files/" + id + ext
}

func publicURLOrigin(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

const schema = `
CREATE TABLE IF NOT EXISTS upload_batches (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT,
  pickup_code TEXT,
  pickup_expires_at TEXT,
  status TEXT NOT NULL DEFAULT 'created',
  total_files INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
  id TEXT PRIMARY KEY,
  batch_id TEXT,
  owner_user_id TEXT,
  original_name TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  image_width INTEGER,
  image_height INTEGER,
  storage_provider TEXT NOT NULL,
  storage_file_id TEXT NOT NULL,
  thumbnail_file_id TEXT,
  storage_url TEXT,
  public_url TEXT NOT NULL,
  is_public INTEGER NOT NULL DEFAULT 1,
  require_confirm INTEGER NOT NULL DEFAULT 0,
  region_policy TEXT NOT NULL DEFAULT 'global',
  hotlink_policy TEXT NOT NULL DEFAULT 'allow',
  status TEXT NOT NULL DEFAULT 'active',
  deleted_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(batch_id) REFERENCES upload_batches(id)
);
CREATE INDEX IF NOT EXISTS idx_files_owner ON files(owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_files_batch ON files(batch_id);
CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256);
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);

CREATE TABLE IF NOT EXISTS file_events (
  id TEXT PRIMARY KEY,
  file_id TEXT,
  batch_id TEXT,
  owner_user_id TEXT,
  event_type TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY(file_id) REFERENCES files(id),
  FOREIGN KEY(batch_id) REFERENCES upload_batches(id)
);
CREATE INDEX IF NOT EXISTS idx_file_events_file ON file_events(file_id, created_at DESC);

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  role TEXT NOT NULL,
  password_hash TEXT,
  disabled INTEGER NOT NULL DEFAULT 0,
  failed_login_count INTEGER NOT NULL DEFAULT 0,
  locked_until TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_login_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_disabled ON users(disabled);

CREATE TABLE IF NOT EXISTS user_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(provider, provider_user_id),
  FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities(user_id);

CREATE TABLE IF NOT EXISTS account_sessions (
  id TEXT PRIMARY KEY,
  session_hash TEXT NOT NULL UNIQUE,
  account_user_id TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  role TEXT NOT NULL,
  user_type TEXT NOT NULL,
  capabilities_json TEXT NOT NULL DEFAULT '{}',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_account_sessions_user ON account_sessions(account_user_id);
CREATE INDEX IF NOT EXISTS idx_account_sessions_expires ON account_sessions(expires_at);

CREATE TABLE IF NOT EXISTS site_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  actor_account_user_id TEXT,
  actor_role TEXT,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT,
  detail_json TEXT NOT NULL DEFAULT '{}',
  ip TEXT,
  user_agent TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_logs(actor_account_user_id, created_at DESC);
`

const accountSchema = `
CREATE TABLE IF NOT EXISTS user_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(provider, provider_user_id),
  FOREIGN KEY(user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities(user_id);
`

const pickupShareSchema = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_upload_batches_pickup_code ON upload_batches(pickup_code);

CREATE TABLE IF NOT EXISTS pickup_shares (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT,
  pickup_code TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pickup_shares_owner ON pickup_shares(owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pickup_shares_code ON pickup_shares(pickup_code);

CREATE TABLE IF NOT EXISTS pickup_share_files (
  share_id TEXT NOT NULL,
  file_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (share_id, file_id),
  FOREIGN KEY(share_id) REFERENCES pickup_shares(id),
  FOREIGN KEY(file_id) REFERENCES files(id)
);
CREATE INDEX IF NOT EXISTS idx_pickup_share_files_file ON pickup_share_files(file_id);
`
