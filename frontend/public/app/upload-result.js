const params = new URLSearchParams(location.search);
const lang = (navigator.language || "zh-CN").toLowerCase().startsWith("zh") ? "zh-CN" : "en";
const L = {
  "zh-CN": { network: "网络连接异常，无法读取上传结果。", pickup: "取件码", validUntil: "前有效", valid24h: "24 小时内有效", batch: "批次", open: "打开", unauthorized: "请先登录查看该上传批次。", forbidden: "当前账户无权查看该上传批次。", pickupNotFound: "取件码不存在或已过期。", notFound: "上传结果不存在或已经过期。", dbError: "读取上传结果失败，请稍后重试。", generic: "无法读取上传结果。" },
  en: { network: "Network error. Could not load the upload result.", pickup: "Pickup code", validUntil: "valid until", valid24h: "Valid for 24 hours", batch: "Batch", open: "Open", unauthorized: "Please sign in to view this upload batch.", forbidden: "This account cannot view this upload batch.", pickupNotFound: "The pickup code does not exist or has expired.", notFound: "The upload result does not exist or has expired.", dbError: "Could not load the upload result. Try again later.", generic: "Could not load the upload result." }
}[lang];
const pickupCode = normalizePickupCode(params.get("code"));
const id = params.get("upload") || location.pathname.split("/").filter(Boolean).pop();
const box = document.querySelector("#result");
const panel = document.querySelector("#result-panel");
if (!pickupCode && !params.get("upload") && !location.pathname.startsWith("/uploads")) {
  if (panel) panel.hidden = true;
} else if (box) {
  if (panel) panel.hidden = false;
try {
  const endpoint = pickupCode
    ? "/api/pickup/" + encodeURIComponent(pickupCode)
    : "/api/uploads/" + encodeURIComponent(id);
  const res = await fetch(endpoint, { cache: "no-store" });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) {
    box.innerHTML = `<div class="pixel-alert">${escapeHtml(resultErrorMessage(json.code, json.error))}</div>`;
  } else {
    box.innerHTML = summaryCard(json.batch, pickupCode) +
      (json.files || []).map((f) => fileRow(f, pickupCode)).join("");
  }
} catch {
  box.innerHTML = `<div class="pixel-alert">${escapeHtml(L.network)}</div>`;
}
}

function summaryCard(batch, fromPickupCode) {
  const code = batch.pickupCode || fromPickupCode || "";
  const expires = batch.pickupExpiresAt ? formatTime(batch.pickupExpiresAt) : L.valid24h;
  const pickup = code
    ? `<div class="pickup-result"><span>${escapeHtml(L.pickup)}</span><strong>${escapeHtml(code)}</strong><small>${escapeHtml(expires)} ${escapeHtml(L.validUntil)}</small></div>`
    : "";
  return `<div class="pixel-alert ok result-summary">
    <div><strong>${escapeHtml(L.batch)} ${escapeHtml(batch.id)}</strong><br><span>${escapeHtml(batch.status)}</span></div>
    ${pickup}
  </div>`;
}

function fileRow(file, code) {
  const url = code ? pickupFilePath(code, file) : (file.publicUrl || publicFilePath(file));
  return `<div class="file-row result-file">
    <div><strong>${escapeHtml(file.originalName)}</strong><br><span class="muted">${escapeHtml(file.mime)} · ${formatSize(file.size)}</span></div>
    <a class="pixel-button secondary compact" href="${escapeAttr(url)}" target="_blank">${escapeHtml(L.open)}</a>
  </div>`;
}

function publicFilePath(file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/f/${file.id}${ext}`;
}

function pickupFilePath(code, file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/pickup/${encodeURIComponent(code)}/${encodeURIComponent(file.id)}${ext}`;
}

function resultErrorMessage(code, fallback) {
  const messages = {
    unauthorized: L.unauthorized,
    forbidden: L.forbidden,
    pickup_not_found: L.pickupNotFound,
    not_found: L.notFound,
    db_error: L.dbError
  };
  return messages[code] || fallback || L.generic;
}

function formatSize(n) {
  n = Number(n || 0);
  if (n > 1048576) return (n / 1048576).toFixed(1) + " MiB";
  if (n > 1024) return (n / 1024).toFixed(1) + " KiB";
  return n + " B";
}

function formatTime(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString(lang, { hour12: false });
}

function normalizePickupCode(value) {
  return String(value || "").toUpperCase().replace(/[^0-9A-Z]/g, "").slice(0, 12);
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
