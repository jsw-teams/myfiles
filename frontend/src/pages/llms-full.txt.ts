export async function GET() {
  return new Response(`# myfiles full context

myfiles is a file service with temporary pickup codes.

AI and crawler policy:
- Public /files and /files/raw links are user-shared file URLs and may be fetched directly when the file remains public.
- Private application surfaces, account-only APIs, and administrative pages are not crawl targets.
- Discovery metadata is public so agents can understand authentication and access boundaries.
- Do not use the retired multipart POST /api/upload endpoint. It now returns r2_direct_upload_required.
- For uploads, follow the R2 direct-upload contract in /openapi.json: POST /api/upload/r2/init with file descriptors, PUT each file or part to the returned presigned R2 URL, then POST /api/upload/r2/complete with the uploadId and uploaded file records. POST /api/upload/r2/cancel aborts unfinished sessions.

Architecture:
- Go backend
- Astro static frontend served by Go
- SQLite metadata
- Unified account login
- Pluggable storage adapter

Public discovery pages:
- /
- /auth.md
- /openapi.json
- /.well-known discovery metadata

User-controlled sharing paths:
- /files/{id}.{ext}
- /files/raw/{id}.{ext}
- /files/{id}.{ext}/info
- /?code={pickupCode}
- /pickup/{pickupCode}/{fileId}.{ext}

Sharing:
- Upload batches receive a 24-hour pickup code.
- Logged-in file owners can create a separate 24-hour pickup-code share for an existing file.
- Owners can revoke pickup-code shares before their natural expiration.
- Anonymous uploads may be allowed by policy. When they are allowed, the same R2 direct-upload flow works without an account session; when disabled, init returns 401.

Noindex:
- /dashboard
- /api
- /admin
- /setup
- /uploads
- /file
- /f
- /pickup
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
