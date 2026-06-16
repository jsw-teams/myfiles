#!/usr/bin/env bash
set -euo pipefail

CONFIG=${CONFIG:-/etc/myfiles/config.json}
LEGACY_CONFIG=${LEGACY_CONFIG:-}
LIMIT=${LIMIT:-0}
DRY_RUN=${DRY_RUN:-0}

usage() {
  cat <<'USAGE'
Usage: migrate-tgbots-to-r2.sh [--config PATH] [--legacy-config PATH] [--limit N] [--dry-run]

Copies active files whose metadata still points at tgbots into Cloudflare R2,
then updates the SQLite row to storage_provider='r2'. Existing R2 objects are
detected with a signed HEAD and only metadata is updated.

Environment overrides:
  MYFILES_TGBOTS_TOKEN, MYFILES_TGBOTS_URL
  MYFILES_R2_ENDPOINT, MYFILES_R2_ACCESS_KEY_ID, MYFILES_R2_SECRET_ACCESS_KEY
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) CONFIG=$2; shift 2 ;;
    --legacy-config) LEGACY_CONFIG=$2; shift 2 ;;
    --limit) LIMIT=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 127; }
}

json_get() {
  local file=$1 key=$2
  [[ -n "$file" && -f "$file" ]] || return 0
  jq -r "$key // empty" "$file"
}

urlencode() {
  jq -rn --arg v "$1" '$v|@uri'
}

sql_quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/''/g")"
}

public_path() {
  local id=$1 name=$2 base ext lower
  base=${name##*/}
  ext=
  if [[ "$base" == *.* ]]; then
    ext=".${base##*.}"
    lower=$(printf '%s' "$ext" | tr '[:upper:]' '[:lower:]')
    if [[ ${#lower} -le 12 && "$lower" != *"/"* && "$lower" != *"\\"* ]]; then
      ext=$lower
    else
      ext=
    fi
  fi
  printf '/files/%s%s' "$id" "$ext"
}

r2_url_for_key() {
  local endpoint=$1 key=$2
  endpoint=${endpoint%/}
  printf '%s/%s' "$endpoint" "$key"
}

r2_head() {
  local url=$1
  curl --max-time 30 --fail --silent --show-error --head \
    --aws-sigv4 'aws:amz:auto:s3' \
    -u "$R2_ACCESS_KEY_ID:$R2_SECRET_ACCESS_KEY" \
    -H 'x-amz-content-sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' \
    "$url" >/dev/null 2>&1
}

r2_put() {
  local url=$1 tmp=$2 mime=$3 id=$4 sha=$5
  curl --max-time "$TIMEOUT_SECONDS" --fail --silent --show-error \
    --aws-sigv4 'aws:amz:auto:s3' \
    -u "$R2_ACCESS_KEY_ID:$R2_SECRET_ACCESS_KEY" \
    -X PUT \
    --upload-file "$tmp" \
    -H "Content-Type: $mime" \
    -H "x-amz-content-sha256: $sha" \
    -H "x-amz-meta-myfiles-file-id: $id" \
    -H "x-amz-meta-myfiles-sha256: $sha" \
    "$url" >/dev/null
}

need jq
need sqlite3
need curl

DB_PATH=$(json_get "$CONFIG" '.database.path')
PUBLIC_BASE=$(json_get "$CONFIG" '.storage.public_base_url')
TIMEOUT_SECONDS=$(json_get "$CONFIG" '.storage.timeout_seconds')
R2_ENDPOINT=${MYFILES_R2_ENDPOINT:-$(json_get "$CONFIG" '.storage.r2_endpoint')}
R2_BUCKET=$(json_get "$CONFIG" '.storage.r2_bucket')
R2_ACCESS_KEY_ID=${MYFILES_R2_ACCESS_KEY_ID:-$(json_get "$CONFIG" '.storage.r2_access_key_id')}
R2_SECRET_ACCESS_KEY=${MYFILES_R2_SECRET_ACCESS_KEY:-$(json_get "$CONFIG" '.storage.r2_secret_access_key')}
TGBOTS_URL=${MYFILES_TGBOTS_URL:-$(json_get "$CONFIG" '.storage.upload_url')}
TGBOTS_TOKEN=${MYFILES_TGBOTS_TOKEN:-$(json_get "$CONFIG" '.storage.api_key')}

if [[ -n "$LEGACY_CONFIG" ]]; then
  TGBOTS_URL=${MYFILES_TGBOTS_URL:-$(json_get "$LEGACY_CONFIG" '.storage.upload_url')}
  TGBOTS_TOKEN=${MYFILES_TGBOTS_TOKEN:-$(json_get "$LEGACY_CONFIG" '.storage.api_key')}
fi

if [[ -z "$TGBOTS_TOKEN" ]]; then
  TGBOTS_TOKEN=$(json_get /etc/myfiles/config.json.bak-r2-20260614-0958 '.storage.api_key')
fi
if [[ -z "$TGBOTS_URL" ]]; then
  TGBOTS_URL=$(json_get /etc/myfiles/config.json.bak-r2-20260614-0958 '.storage.upload_url')
fi

if [[ -z "$DB_PATH" || -z "$PUBLIC_BASE" || -z "$R2_ENDPOINT" || -z "$R2_ACCESS_KEY_ID" || -z "$R2_SECRET_ACCESS_KEY" ]]; then
  echo "missing required database, public base, or R2 configuration" >&2
  exit 1
fi
if [[ -z "$TGBOTS_URL" || -z "$TGBOTS_TOKEN" ]]; then
  echo "missing legacy tgbots source config; set MYFILES_TGBOTS_URL and MYFILES_TGBOTS_TOKEN" >&2
  exit 1
fi
if [[ -z "$TIMEOUT_SECONDS" || "$TIMEOUT_SECONDS" == "0" ]]; then
  TIMEOUT_SECONDS=120
fi

R2_ENDPOINT=${R2_ENDPOINT%/}
if [[ -n "$R2_BUCKET" && "$R2_ENDPOINT" != */"$R2_BUCKET" ]]; then
  R2_ENDPOINT="$R2_ENDPOINT/$R2_BUCKET"
fi
PUBLIC_BASE=${PUBLIC_BASE%/}
TGBOTS_URL=${TGBOTS_URL%/}

query="SELECT id, original_name, mime, size, sha256, storage_file_id FROM files WHERE storage_provider='tgbots' ORDER BY created_at ASC"
if [[ "$LIMIT" =~ ^[0-9]+$ && "$LIMIT" -gt 0 ]]; then
  query="$query LIMIT $LIMIT"
fi

tmpdir=$(mktemp -d /tmp/myfiles-r2-migrate.XXXXXX)
trap 'rm -rf "$tmpdir"' EXIT

total=0
migrated=0
skipped=0

while IFS=$'\t' read -r id name mime size sha storage_file_id; do
  [[ -n "$id" ]] || continue
  total=$((total + 1))
  path=$(public_path "$id" "$name")
  key=${path#/}
  r2_url=$(r2_url_for_key "$R2_ENDPOINT" "$key")
  public_url="$PUBLIC_BASE$path"
  printf '[%d] %s -> %s (%s bytes)\n' "$total" "$id" "$key" "$size"

  if [[ "$DRY_RUN" == "1" ]]; then
    skipped=$((skipped + 1))
    continue
  fi

  if r2_head "$r2_url"; then
    sqlite3 "$DB_PATH" "UPDATE files SET storage_provider='r2', storage_file_id=$(sql_quote "$key"), storage_url=$(sql_quote "$key"), public_url=$(sql_quote "$public_url"), updated_at=datetime('now') WHERE id=$(sql_quote "$id");"
    migrated=$((migrated + 1))
    continue
  fi

  tmp="$tmpdir/$id"
  fetch_url="$TGBOTS_URL/fetch?bot_token=$(urlencode "$TGBOTS_TOKEN")&file_id=$(urlencode "$storage_file_id")"
  curl --max-time "$TIMEOUT_SECONDS" --fail --silent --show-error -o "$tmp" "$fetch_url"
  actual_size=$(wc -c < "$tmp" | tr -d ' ')
  if [[ "$actual_size" != "$size" ]]; then
    echo "warning: size mismatch for $id: got $actual_size want $size; uploading fetched source bytes" >&2
  fi
  actual_sha=$(sha256sum "$tmp" | awk '{print $1}')
  if [[ "$actual_sha" != "$sha" ]]; then
    echo "warning: sha256 mismatch for $id: got $actual_sha want $sha; using fetched source hash for R2 signature" >&2
  fi
  r2_put "$r2_url" "$tmp" "$mime" "$id" "$actual_sha"
  sqlite3 "$DB_PATH" "UPDATE files SET storage_provider='r2', storage_file_id=$(sql_quote "$key"), storage_url=$(sql_quote "$key"), public_url=$(sql_quote "$public_url"), updated_at=datetime('now') WHERE id=$(sql_quote "$id");"
  migrated=$((migrated + 1))
  rm -f "$tmp"
done < <(sqlite3 -separator $'\t' "$DB_PATH" "$query;")

printf 'done: scanned=%d migrated=%d dry_run_skipped=%d\n' "$total" "$migrated" "$skipped"
