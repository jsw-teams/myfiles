# myfiles

`myfiles` is a compact file-sharing service built with Go and Astro. It provides public uploads, 24-hour pickup codes, a unified account login flow, personal file management, permissioned administration, audit logs, and an OpenResty front door.

The current UI uses a unified pixel style with a friendly black bear mascot. Site name, page title, SEO text, visible language, and share-facing labels are driven by site configuration and the visitor browser language instead of hard-coded bilingual text.

## Routes

| Route | Purpose |
| --- | --- |
| `/` | Public upload and pickup-code shortcut |
| `/login` | Unified account login entry |
| `/dashboard` | User console and permissioned admin console |
| `/files/{fileId}.{ext}` | Canonical file preview/open URL |
| `/pickup/{pickupCode}/{fileId}.{ext}` | Canonical pickup-code file preview/open URL |

Legacy console and upload-result paths redirect to these routes:

- `/d/files` -> `/dashboard#files`
- `/a/files` -> `/dashboard#admin-files`
- `/a/audit` -> `/dashboard#audit`
- `/a/settings` -> `/dashboard#settings`
- `/uploads/{batch}` -> `/?upload={batch}`

## User Flow

- Visitors upload from `/`.
- Pending files can be removed before upload starts.
- Active uploads run in a modal with real-time progress and a cancel action.
- A successful upload creates a 24-hour pickup code and redirects to `/?upload={batchId}`.
- `/?upload={batchId}` shows the pickup code, a copy-link button, and the uploaded file list.
- Pickup links use `/?code={pickupCode}`.
- Pickup file access uses `/pickup/{pickupCode}/{fileId}`.
- Files use `/files/{fileId}.{ext}` for preview/open. Previewable media opens in the myfiles preview page with responsive media sizing; files that cannot be previewed online show an in-page safety confirmation before download.
- File rows use type-specific visual badges for images, video, audio, archives, code, documents, spreadsheets, and presentations.
- Logged-in users can manage their own files, create pickup codes, and expire pickup codes early.

## Dashboard

`/dashboard` is the only console route. It keeps the same pixel UI, language behavior, icons, and layout rules across the overview, personal files, all files, audit logs, and settings.

Permissioned views are hidden from users without access:

- Personal files: visible to signed-in users.
- All files: requires all-file read permission.
- Audit logs: requires audit permission.
- Site settings: requires settings write permission.

Personal files and all-file management support selection and batch operations. Batch endpoints are used so the browser does not send one request per selected file.

## API Highlights

| Endpoint | Purpose |
| --- | --- |
| `POST /api/upload/r2/init` | Validate files, create an R2 upload session, and return presigned part URLs |
| `POST /api/upload/r2/complete` | Complete uploaded R2 objects and create a batch pickup code |
| `POST /api/upload/r2/cancel` | Abort an unfinished R2 upload session |
| `GET /api/me` | Current account and permission snapshot |
| `GET /api/files` | Current user's files |
| `POST /api/files/batch` | Batch delete or share current user's selected files |
| `GET /api/admin/files` | Permissioned all-file listing |
| `POST /api/admin/files/batch` | Permissioned batch delete and policy changes |
| `GET /api/admin/audit?limit=50&ip=` | Audit log listing with row limit and IP search |
| `GET /api/settings` | Site settings snapshot |
| `POST /api/settings` | Permissioned site settings update |

## Settings

The settings page is grouped into focused sections so long configuration does not create a single oversized form. The UI avoids raw text inputs where a constrained control is safer.

Important settings:

- Site name, title, SEO description, and geo text.
- Upload policy and anonymous upload availability.
- Default file visibility and download-confirm behavior.
- Region policy, with one active mode only:
  - `global`
  - `allow:CN,US`
  - `deny:CN,US`
- Hotlink policy, controlled by UI options.
- Session TTL.
- Audit retention days.

Public file links are inferred from the current request origin when an explicit base URL is not configured. The old public-domain helper text is intentionally not shown in the settings UI.

## Configuration

Production config is normally stored at:

```text
/etc/myfiles/config.json
```

Common fields:

- `app.name`: displayed site name.
- `app.base_url`: optional public base URL; request origin is used when empty.
- `app.public_dir`: Astro build output directory.
- `database.path`: SQLite metadata path.
- `account.client_id`: unified account client id.
- `account.client_secret`: unified account secret.
- `account.redirect_uri`: login callback URL.
- `storage.mode`: `r2`, `local`, or `disabled`.
- `storage.r2_endpoint`: Cloudflare R2 S3 API endpoint, usually ending with the bucket name.
- `storage.r2_bucket`: R2 bucket name.
- `storage.r2_access_key_id`: R2 S3 access key id.
- `storage.r2_secret_access_key`: R2 S3 secret access key.
- `storage.r2_region`: R2 signing region, normally `auto`.
- `storage.r2_prefix`: optional object key prefix.
- R2 bucket CORS must allow `GET`, `HEAD`, `PUT`, `POST`, and `DELETE` from `https://files.js.gripe`; expose `ETag` so multipart browser uploads can complete.
- `file.default_region_policy`: `global`, `allow:<codes>`, or `deny:<codes>`.
- `file.default_hotlink_policy`: `allow` or `deny`.
- `security.session_ttl_hours`: browser session duration.
- `audit.retention_days`: audit retention setting.

Account-system third-party login registration values are documented in:

```text
configs/account.integration.example.json
```

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
go build -o /opt/myfiles/bin/myfilesd ./cmd/myfilesd
```

## Deploy

Service checks:

```bash
systemctl status myfiles.service
systemctl status openresty
journalctl -u myfiles.service -f
```

After code changes:

```bash
cd /opt/myfiles
go test ./...
go build -o /opt/myfiles/bin/myfilesd ./cmd/myfilesd
systemctl restart myfiles.service
```

After frontend-only changes:

```bash
cd /opt/myfiles/frontend
npm run build
systemctl restart myfiles.service
```

OpenResty should stay on the newest package available from the configured OpenResty repository:

```bash
openresty -v
openresty -t
systemctl is-enabled openresty
systemctl is-active openresty
systemctl reload openresty
```

The current deployment was checked against the packaged OpenResty `1.29.2.3` line.

## Caching

The Go service serves dynamic HTML and API responses with private/no-store behavior where appropriate. Versioned browser scripts, generated icons, mascot images, and hashed Astro assets can be cached publicly to reduce origin pressure.

The OpenResty snippet in `deploy/openresty/files.js.gripe.snippet.conf` keeps account callback token query strings out of access logs and forwards traffic to the local `myfilesd` service.

## Verification

Health check through the public host:

```bash
curl https://files.js.gripe/healthz
```

Origin-local checks:

```bash
curl -k --resolve files.js.gripe:443:127.0.0.1 https://files.js.gripe/healthz
curl -k --resolve files.js.gripe:443:127.0.0.1 https://files.js.gripe/dashboard
curl -k --resolve files.js.gripe:443:127.0.0.1 https://files.js.gripe/download
```

## Security Notes

- `myfiles_session` is HTTP-only and stored server-side as a hash.
- Account callback tokens are not logged by the OpenResty access log format.
- Admin actions write audit entries with actor, target, IP address, user agent, and detail payload.
- Non-image downloads require explicit confirmation unless opened through an external storage redirect.
- All-file management, audit logs, and settings remain permission gated in both UI and API routes.
