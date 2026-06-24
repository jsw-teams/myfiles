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
Allow: /llms.txt
Allow: /llms-full.txt
Allow: /openapi.json
Allow: /auth.md
Allow: /.well-known/
Allow: /files/
# Public /files and /files/raw links intentionally stay crawlable so public
# shared files can be fetched directly. Private application surfaces are not
# useful crawl targets. API clients should read /openapi.json rather than
# crawling /api/; current uploads use /api/upload/r2/init, presigned R2 PUTs,
# and /api/upload/r2/complete.
Disallow: /api/
Disallow: /admin
Disallow: /setup
Disallow: /dashboard/
Disallow: /uploads/
Disallow: /file/
Disallow: /f/
Disallow: /pickup/
Sitemap: https://files.js.gripe/sitemap.xml
`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
}
