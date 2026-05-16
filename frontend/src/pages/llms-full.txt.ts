export async function GET() {
  return new Response(`# myfiles full context

myfiles is a file service with temporary pickup codes.

Architecture:
- Go backend
- Astro static frontend served by Go
- SQLite metadata
- Unified account login
- Pluggable storage adapter

Public pages:
- /
- /file/{id}
- /file/{id}/info
- /?code={pickupCode}
- /pickup/{pickupCode}/{fileId}

Sharing:
- Upload batches receive a 24-hour pickup code.
- Logged-in file owners can create a separate 24-hour pickup-code share for an existing file.
- Owners can revoke pickup-code shares before their natural expiration.

Noindex:
- /dashboard
- /api
- /admin
- /setup
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
