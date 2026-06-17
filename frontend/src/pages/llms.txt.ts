export async function GET() {
  return new Response(`# myfiles

myfiles is a file service with temporary pickup codes.

Public purpose:
- Upload and share files through controlled file links and 24-hour pickup codes.
- Logged-in users can manage their own files.
- File owners can create temporary pickup-code shares and revoke them early.
- Administrators can audit and manage files according to policy.

Crawler and AI-use policy:
- Public /files and /files/raw links are directly fetchable while the file is public.
- Agents may read public file links, the public homepage, and discovery/authentication metadata.
- Do not crawl private application surfaces: /pickup, /file, /f, /uploads, /dashboard, /api, /admin, or /setup.

Allowed discovery references:
- https://files.js.gripe/auth.md
- https://files.js.gripe/.well-known/api-catalog
- https://files.js.gripe/.well-known/oauth-protected-resource
- https://files.js.gripe/.well-known/oauth-authorization-server
- https://files.js.gripe/.well-known/mcp/server-card.json
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
