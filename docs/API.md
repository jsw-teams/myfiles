# API contract

All JSON errors use:

```json
{ "ok": false, "code": "string", "error": "safe user message", "detail": {} }
```

## Public

- `GET /api/bootstrap`
- `POST /api/upload`
- `GET /api/uploads/{batchId}`
- `GET /api/pickup/{pickupCode}`
- `GET /files/{id}.{ext}`
- `POST /files/{id}.{ext}/download-confirm`
- `GET /files/{id}.{ext}/download`
- `GET /files/{id}.{ext}/info`
- `POST /files/{id}.{ext}/confirm`
- `GET /pickup/{pickupCode}/{fileId}.{ext}`
- `POST /pickup/{pickupCode}/{fileId}.{ext}/download-confirm`
- `GET /pickup/{pickupCode}/{fileId}.{ext}/download`

`/files/{id}.{ext}` is the canonical preview/open URL. Previewable media opens in the myfiles preview page while media element requests on the same URL stream the original bytes with range support. Files that cannot be previewed online show an in-page safety confirmation; confirmed downloads are authorized by the application with a short-lived cookie rather than a URL parameter.

## Account

- `GET /auth/account/start?popup=1`
- `GET /auth/account/callback`
- `GET /api/account/me`
- `POST /api/auth/logout`

## User files

- `GET /api/files?q=`
- `GET /api/files/{id}`
- `POST /api/files/{id}/share`
- `DELETE /api/shares/{pickupCode}`
- `DELETE /api/files/{id}`

## Admin

- `GET /api/admin/files?q=&owner=`
- `PATCH /api/admin/files/{id}`
- `DELETE /api/admin/files/{id}`
- `GET /api/admin/audit`
- `GET /api/admin/settings`
- `PATCH /api/admin/settings`
