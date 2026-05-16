export async function GET() {
  return new Response(`# myfiles

myfiles is a file service with temporary pickup codes.

Public purpose:
- Upload and share files through controlled file links and 24-hour pickup codes.
- Logged-in users can manage their own files.
- File owners can create temporary pickup-code shares and revoke them early.
- Administrators can audit and manage files according to policy.

Public helper paths:
- /?code=<pickupCode> resolves a valid pickup code into a temporary file list.
- /pickup/<pickupCode>/<fileId> streams a file while the pickup code is valid.

Private paths such as /dashboard, /api, /admin and /setup are not intended for indexing.
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
