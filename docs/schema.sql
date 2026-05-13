CREATE TABLE upload_batches (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT,
  status TEXT NOT NULL DEFAULT 'created',
  total_files INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE files (
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

CREATE INDEX idx_files_owner ON files(owner_user_id, created_at DESC);
CREATE INDEX idx_files_batch ON files(batch_id);
CREATE INDEX idx_files_sha256 ON files(sha256);
CREATE INDEX idx_files_status ON files(status);

CREATE TABLE file_events (
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

CREATE INDEX idx_file_events_file ON file_events(file_id, created_at DESC);

CREATE TABLE account_sessions (
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

CREATE INDEX idx_account_sessions_user ON account_sessions(account_user_id);
CREATE INDEX idx_account_sessions_expires ON account_sessions(expires_at);

CREATE TABLE site_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE storage_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE audit_logs (
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

CREATE INDEX idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_actor ON audit_logs(actor_account_user_id, created_at DESC);
