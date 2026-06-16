#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";

const args = new Set(process.argv.slice(2));
const write = args.has("--write");
const modeArg = process.argv.find((item) => item.startsWith("--mode="));
const mode = modeArg ? modeArg.slice("--mode=".length) : "raw-all";
const rootArg = process.argv.find((item) => item.startsWith("--root="));
const root = path.resolve(rootArg ? rootArg.slice("--root=".length) : "/opt/myblog");
const exts = new Set([".md", ".mdx", ".json", ".mjs", ".js", ".astro"]);
const urlRe = /https:\/\/files\.js\.gripe\/(f|files)\/([^\s"'<>),\]]+)/g;

if (!["raw-all", "legacy-f-only"].includes(mode)) {
  console.error("mode must be raw-all or legacy-f-only");
  process.exit(2);
}

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === ".git" || entry.name === "public" || entry.name === "releases") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, out);
    else if (exts.has(path.extname(entry.name))) out.push(full);
  }
  return out;
}

function rewriteURL(prefix, rest) {
  if (prefix === "f") return `https://files.js.gripe/files/raw/${rest}`;
  if (mode === "raw-all" && !rest.startsWith("raw/") && !rest.includes("/confirm") && !rest.includes("/download")) {
    return `https://files.js.gripe/files/raw/${rest}`;
  }
  return `https://files.js.gripe/${prefix}/${rest}`;
}

const changes = [];
for (const file of walk(root)) {
  const before = fs.readFileSync(file, "utf8");
  let count = 0;
  const after = before.replace(urlRe, (match, prefix, rest) => {
    const next = rewriteURL(prefix, rest);
    if (next !== match) count += 1;
    return next;
  });
  if (count > 0) {
    changes.push({ file, count });
    if (write) fs.writeFileSync(file, after);
  }
}

const total = changes.reduce((sum, item) => sum + item.count, 0);
console.log(JSON.stringify({ ok: true, root, mode, write, files: changes.length, replacements: total, changes }, null, 2));
if (!write && total > 0) {
  console.log("dry-run only; rerun with --write to apply");
}
