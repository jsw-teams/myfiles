const id = location.pathname.split("/").filter(Boolean).pop();
const box = document.querySelector("#result");
try {
  const res = await fetch("/api/uploads/" + encodeURIComponent(id), { cache: "no-store" });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    box.innerHTML = `<div class="pixel-alert">${escapeHtml(resultErrorMessage(json.code, json.error))}</div>`;
  } else {
    box.innerHTML = `<div class="pixel-alert ok">批次 ${escapeHtml(json.batch.id)}：${escapeHtml(json.batch.status)}</div>` +
      (json.files || []).map((f) => fileRow(f)).join("");
  }
} catch {
  box.innerHTML = `<div class="pixel-alert">网络连接异常，无法读取上传结果。</div>`;
}

function fileRow(file) {
  const url = file.publicUrl || publicFilePath(file);
  return `<div class="file-row"><strong>${escapeHtml(file.originalName)}</strong><br>${escapeHtml(file.mime)} · ${Number(file.size || 0)} bytes<br><a href="${escapeAttr(url)}" target="_blank">${escapeHtml(url)}</a></div>`;
}

function publicFilePath(file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/f/${file.id}${ext}`;
}

function resultErrorMessage(code, fallback) {
  const messages = {
    unauthorized: "请先使用统一账户登录查看该上传批次。",
    forbidden: "当前账户无权查看该上传批次。",
    not_found: "上传结果不存在或已经过期。",
    db_error: "读取上传结果失败，请稍后重试。"
  };
  return messages[code] || fallback || "无法读取上传结果。";
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;"
  })[char]);
}

function escapeAttr(value) {
  return escapeHtml(value).replace(/`/g, "&#96;");
}
