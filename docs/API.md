# API contract

All JSON errors use:

```json
{ "ok": false, "code": "string", "error": "safe user message", "detail": {} }
```

## Public

- `GET /api/bootstrap`
- `POST /api/upload/r2/init`
- `PUT {presigned R2 URL returned by /api/upload/r2/init}`
- `POST /api/upload/r2/complete`
- `POST /api/upload/r2/cancel`
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

Uploads no longer use the retired multipart `POST /api/upload` endpoint. The current flow is:

1. Call `POST /api/upload/r2/init` with file descriptors: `clientId`, `name`, `size`, `type`, and optional `sha256`.
2. Upload each file, or each multipart part, to the presigned R2 URL(s) returned by `init`.
3. Call `POST /api/upload/r2/complete` with `uploadId` and uploaded file records containing `clientId`, `fileId`, `etag`, and multipart `parts` when applicable.
4. Call `POST /api/upload/r2/cancel` to abort an unfinished upload session.

Anonymous uploads may be accepted when policy allows them. If anonymous upload is disabled, `init` returns `401`.

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
