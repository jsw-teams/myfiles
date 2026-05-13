export async function GET() {
  return new Response(`# myfiles full context

myfiles is a JS.Gripe business file service replacing the old mypicture project.

Architecture:
- Go backend
- Astro static frontend served by Go
- SQLite metadata
- Account-system unified login
- tgbots storage adapter through gateway.js.gripe/api/v1/tgbots

Public pages:
- /
- /file/{id}
- /file/{id}/info

Noindex:
- /dashboard
- /api
- /admin
- /setup
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
