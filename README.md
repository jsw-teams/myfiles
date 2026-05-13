# myfiles

`myfiles` is the JS.Gripe file service at `https://files.js.gripe`. It replaces the older picture/file experiments and uses account-system for identity.

Current production shape:

- Backend: Go service `myfilesd`
- Frontend: Astro static UI served by `myfilesd`
- Style: bright pixel UI with JS.Gripe black bear assets
- Database: SQLite metadata and audit logs
- Storage: local disk or `tgbots` through `gateway.js.gripe/api/v1/tgbots`
- Identity: account-system third-party login
- Reverse proxy: OpenResty on `files.js.gripe`

## Features

- Anonymous upload can be enabled by policy.
- Logged-in users manage their own files.
- Admins can view all files, open files even when hidden/not public, edit file attributes, and soft-delete files.
- File attributes include public access, confirmation requirement, region policy, hotlink policy, and status.
- Public short links use `/f/{id}.{ext}` for better cache hit rates.
- Upload and download UI show progress.
- Settings are saved per section. Storage settings are tested before activation.

## Main Paths

```text
cmd/myfilesd/                         Go entrypoint
internal/server/                      HTTP routes and admin/API logic
internal/files/                       file metadata model
internal/storage/                     local and tgbots storage adapters
internal/account/                     account-system client
internal/audit/                       audit log writer
frontend/                            Astro frontend
configs/config.example.json           service config example
configs/account.integration.example.json
```

## Configuration

Production config is JSON, normally:

```text
/etc/myfiles/config.json
```

Important fields:

- `app.base_url`: `https://files.js.gripe`
- `app.public_dir`: `/opt/myfiles/frontend/dist`
- `database.path`: `/var/lib/myfiles/myfiles.sqlite3`
- `account.client_id`: account-system client id
- `account.client_secret`: account-system API key/client secret
- `account.redirect_uri`: `https://files.js.gripe/auth/account/callback`
- `storage.mode`: `tgbots`, `local`, or `disabled`
- `storage.upload_url`: normally `https://gateway.js.gripe/api/v1/tgbots`
- `storage.api_key`: Telegram bot token for tgbots mode
- `storage.chat_id`: Telegram chat/group id used for upload/fetch source
- `security.session_ttl_hours`: browser session duration

Account-system registration values are documented in:

```text
configs/account.integration.example.json
```

Create a client in account-system with:

- application name: `myfiles`
- redirect URI: `https://files.js.gripe/auth/account/callback`
- scopes: `accounts:read`, `identities:resolve`, `identities:link`

## tgbots Storage

`myfiles` calls the Telegram Bot API shaped path:

```text
https://gateway.js.gripe/api/v1/tgbots/bot<TOKEN>/sendDocument
```

The Go service resolves `gateway.js.gripe` locally to OpenResty, so the request stays on the VPS and OpenResty/Lua can apply local auth, limits, field conversion, and local Telegram Bot API routing.

Downloads use:

```text
https://gateway.js.gripe/api/v1/tgbots/fetch?bot_token=...&file_id=...
```

The upload path is streamed with multipart `io.Pipe`; files are not fully buffered in memory before being sent to tgbots.

## Build

Frontend:

```bash
cd /opt/myfiles/frontend
npm run build
```

Backend:

```bash
cd /opt/myfiles
go test ./...
go build -buildvcs=false -o /opt/myfiles/bin/myfilesd ./cmd/myfilesd
```

## Deploy

Production service:

```bash
systemctl status myfiles.service
journalctl -u myfiles.service -f
```

After backend changes:

```bash
go build -buildvcs=false -o /tmp/myfilesd ./cmd/myfilesd
sudo install -m 0755 -o myfiles -g myfiles /tmp/myfilesd /opt/myfiles/bin/myfilesd
sudo systemctl restart myfiles.service
```

The service writes `/etc/myfiles/config.json` from the admin settings page. With `ProtectSystem=full`, systemd must allow:

```text
ReadWritePaths=/var/lib/myfiles /etc/myfiles
```

The config directory should be writable by group `myfiles`.

## Verification

```bash
curl https://files.js.gripe/healthz
```

For origin-local checks:

```bash
curl -k --resolve files.js.gripe:443:127.0.0.1 https://files.js.gripe/healthz
```

Check tgbots gateway:

```bash
curl -k --resolve gateway.js.gripe:443:127.0.0.1 https://gateway.js.gripe/api/v1/tgbots/healthz
```

## Security Notes

- `myfiles_session` is HTTP-only and stored server-side as a hash.
- Account-system `account_session` is used only during callback validation.
- `/auth/account/callback` should not be logged by OpenResty because it receives the account session token.
- Admin actions write audit logs.
- HTML and `/app/*.js` are served `no-store`; built CSS/JS assets use versioned or hashed URLs.
