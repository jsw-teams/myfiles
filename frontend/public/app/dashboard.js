async function api(path, opts) {
  const res = await fetch(path, opts);
  const json = await res.json().catch(() => ({}));
  if (!res.ok || json.ok === false) throw new Error(json.error || "请求失败");
  return json;
}
const nav = document.querySelector("#nav");
const cards = document.querySelector("#cards");
try {
  const me = await api("/api/account/me");
  const user = me.user;
  const p = me.myfilesPermissions;
  document.querySelector("#profile").textContent = `${user.displayName || user.email || user.id} · ${user.role || user.userType}`;
  const links = [{href:"/d/files", title:"我的文件", desc:"查看、搜索和删除自己的文件。"}];
  if (p.allFilesRead) links.push({href:"/a/files", title:"全部文件管理", desc:"按 owner 搜索和代管文件。"});
  if (p.batchesRead) links.push({href:"/a/files", title:"上传批次", desc:"查看上传批次与文件状态。"});
  if (p.auditRead) links.push({href:"/a/audit", title:"审计日志", desc:"查看代管和系统行为。"});
  if (p.settingsRead) links.push({href:"/a/settings", title:"站点设置", desc:"管理站点、存储通道和 CDN 设置。"});
  nav.innerHTML = links.map(l => `<a href="${l.href}">${l.title}</a>`).join("") + `<button id="logout">退出登录</button>`;
  cards.innerHTML = links.map(l => `<a class="pixel-card" href="${l.href}"><h3>${l.title}</h3><p style="color:var(--muted)">${l.desc}</p></a>`).join("");
  document.querySelector("#logout").onclick = async () => { await api("/api/auth/logout", {method:"POST"}); location.href="/"; };
} catch(e) {
  location.href = "/login";
}
