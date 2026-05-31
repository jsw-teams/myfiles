export async function GET() {
  return new Response(`# Claude is not welcome here because this site owner does not welcome
# unethical AI crawlers that freely scrape sites while arbitrarily
# banning user accounts.
User-agent: ClaudeBot
Disallow: /

User-agent: Claude-User
Disallow: /

User-agent: *
Allow: /
# Private application surfaces are not useful crawl targets.
Disallow: /api/
Disallow: /admin
Disallow: /setup
Disallow: /dashboard/
Sitemap: https://files.js.gripe/sitemap.xml
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
