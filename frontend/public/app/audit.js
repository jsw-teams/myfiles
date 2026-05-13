const rows = document.querySelector("#rows");
const res = await fetch("/api/admin/audit");
const json = await res.json().catch(() => ({}));
rows.innerHTML = res.ok ? (json.logs || []).map(l => `<tr><td>${l.createdAt}</td><td>${l.actorAccountUserId}<br>${l.actorRole}</td><td>${l.action}</td><td>${l.targetType}<br>${l.targetId}</td></tr>`).join("") : `<tr><td colspan="4">${json.error || "无权访问"}</td></tr>`;
