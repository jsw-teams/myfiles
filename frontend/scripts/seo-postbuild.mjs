import { existsSync, copyFileSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const dist = new URL("../dist/", import.meta.url).pathname;
const sitemapIndex = join(dist, "sitemap-index.xml");
const sitemapXml = join(dist, "sitemap.xml");
const robotsTxt = join(dist, "robots.txt");

if (existsSync(sitemapIndex)) {
  copyFileSync(sitemapIndex, sitemapXml);
}

if (existsSync(robotsTxt)) {
  let robots = readFileSync(robotsTxt, "utf8");
  const sitemapLine = "Sitemap: https://files.js.gripe/sitemap.xml";

  if (!robots.includes(sitemapLine)) {
    robots = robots.trimEnd() + "\n" + sitemapLine + "\n";
    writeFileSync(robotsTxt, robots);
  }
}
