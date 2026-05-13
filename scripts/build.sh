#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[frontend] install/build"
cd "$ROOT/frontend"

if [ -f package-lock.json ]; then
  npm ci
else
  npm install --no-audit --no-fund
fi

npm run build

echo "[go] build"
cd "$ROOT"
mkdir -p "$ROOT/bin"
go mod tidy
go build -buildvcs=false -trimpath -ldflags="-s -w" -o "$ROOT/bin/myfilesd" ./cmd/myfilesd

echo "[ok] built $ROOT/bin/myfilesd"
