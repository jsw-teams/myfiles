#!/usr/bin/env bash
set -euo pipefail

CONFIG_PATH="${CONFIG_PATH:-/etc/myfiles/config.json}"
DB_PATH="${DB_PATH:-}"
BACKUP_DIR="${BACKUP_DIR:-/opt/myfiles/backups/v2-r2-only-$(date -u +%Y%m%dT%H%M%SZ)}"
SERVICE_NAME="${SERVICE_NAME:-myfiles.service}"

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 2; }
}

need jq
need sqlite3

if [[ ! -r "$CONFIG_PATH" ]]; then
  echo "cannot read config: $CONFIG_PATH" >&2
  exit 1
fi

mode="$(jq -r '.storage.mode // ""' "$CONFIG_PATH")"
bucket="$(jq -r '.storage.r2_bucket // ""' "$CONFIG_PATH")"
endpoint="$(jq -r '.storage.r2_endpoint // ""' "$CONFIG_PATH")"
public_base="$(jq -r '.storage.r2_public_base_url // ""' "$CONFIG_PATH")"

if [[ "$mode" != "r2" || -z "$bucket" || -z "$endpoint" ]]; then
  echo "storage must be R2-only before migration (mode=$mode bucket=$bucket endpoint=$endpoint)" >&2
  exit 1
fi

if [[ -z "$DB_PATH" ]]; then
  DB_PATH="$(jq -r '.database.path // ""' "$CONFIG_PATH")"
fi
if [[ -z "$DB_PATH" || ! -f "$DB_PATH" ]]; then
  echo "database path not found: $DB_PATH" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"
cp -a "$CONFIG_PATH" "$BACKUP_DIR/config.json"
sqlite3 "$DB_PATH" ".backup '$BACKUP_DIR/myfiles.sqlite3'"

sqlite3 "$DB_PATH" <<'SQL'
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
UPDATE files SET status='deleted', deleted_at=COALESCE(deleted_at, updated_at) WHERE status='soft_deleted';
UPDATE files SET storage_provider='r2' WHERE storage_provider IN ('telegram', 'tgbots', 'telegram-bot') AND storage_file_id LIKE 'files/%';
SQL

cat <<EOF
R2-only migration prepared.
backup: $BACKUP_DIR
db: $DB_PATH
storage: $endpoint / $bucket
public R2 base: ${public_base:-"(not set)"}

Next:
  systemctl restart $SERVICE_NAME
  /opt/myfiles/bin/r2reconcile -config "$CONFIG_PATH"
  node /opt/myfiles/scripts/myblog-file-url-migration.mjs --root=/opt/myblog
  node /opt/myfiles/scripts/myblog-file-url-migration.mjs --root=/opt/myblog --write
EOF
