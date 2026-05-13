export async function GET() {
  return new Response(`User-agent: *
Allow: /
Disallow: /api/
Disallow: /admin
Disallow: /setup
Disallow: /dashboard/
Sitemap: https://files.js.gripe/sitemap.xml
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
