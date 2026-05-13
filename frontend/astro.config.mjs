import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";

const site = "https://files.js.gripe";

const blockedPrefixes = [
  "/dashboard",
  "/admin",
  "/api",
  "/setup",
  "/uploads"
];

export default defineConfig({
  site,
  output: "static",
  integrations: [
    sitemap({
      filter: (page) => {
        const url = new URL(page);
        return !blockedPrefixes.some((prefix) => url.pathname.startsWith(prefix));
      },
      serialize(item) {
        const url = new URL(item.url);

        if (url.pathname === "/") {
          item.changefreq = "weekly";
          item.priority = 1.0;
        } else if (
          url.pathname.startsWith("/md/") ||
          url.pathname === "/llms.txt" ||
          url.pathname === "/llms-full.txt"
        ) {
          item.changefreq = "weekly";
          item.priority = 0.7;
        } else {
          item.changefreq = "monthly";
          item.priority = 0.5;
        }

        return item;
      }
    })
  ]
});
