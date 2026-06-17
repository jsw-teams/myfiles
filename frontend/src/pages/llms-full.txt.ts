export async function GET() {
  return new Response(`# myfiles full context

myfiles is a file service with temporary pickup codes.

AI and crawler policy:
- Public /files and /files/raw links are user-shared file URLs and may be fetched directly when the file remains public.
- Private application surfaces, account-only APIs, and administrative pages are not crawl targets.
- Discovery metadata is public so agents can understand authentication and access boundaries.

Architecture:
- Go backend
- Astro static frontend served by Go
- SQLite metadata
- Unified account login
- Pluggable storage adapter

Public discovery pages:
- /
- /auth.md
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
