export async function GET() {
  return new Response(`Content-Signal: ai-train=no, search=no, ai-input=no

# Claude is not welcome here because this site owner does not welcome
# unethical AI crawlers that freely scrape sites while arbitrarily
# banning user accounts.
User-agent: ClaudeBot
Disallow: /

User-agent: Claude-User
Disallow: /

User-agent: *
Allow: /
# Private application surfaces and user-controlled file links are not useful crawl targets.
Disallow: /api/
Disallow: /admin
Disallow: /setup
Disallow: /dashboard/
Disallow: /uploads/
Disallow: /file/
Disallow: /f/
Disallow: /files/
Disallow: /pickup/
Sitemap: https://files.js.gripe/sitemap.xml
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
