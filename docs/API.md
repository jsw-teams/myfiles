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
- `GET /pickup/{pickupCode}/{fileId}`
- `GET /file/{id}`
- `GET /file/{id}/info`
- `POST /file/{id}/confirm`

Non-image files served from local storage or tgbots show a download confirmation page first. Use `?download=1` after user confirmation to stream the file as an attachment. External storage links are redirected without this confirmation page.

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
