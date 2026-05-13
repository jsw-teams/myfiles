export async function GET() {
  return new Response(`# myfiles

myfiles is the JS.Gripe file service at https://files.js.gripe.

Public purpose:
- Upload and share files through controlled file links.
- Logged-in users can manage their own files.
- Administrators can audit and manage files according to policy.

Private paths such as /dashboard, /api, /admin and /setup are not intended for indexing.
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
