const params = new URLSearchParams(location.search);
const lang = (navigator.language || "zh-CN").toLowerCase().startsWith("zh") ? "zh-CN" : "en";
const L = {
  "zh-CN": { network: "网络连接异常，无法读取上传结果。", pickup: "取件码", validUntil: "前有效", valid24h: "24 小时内有效", batch: "批次", open: "打开", preview: "预览", copyPreview: "复制预览链接", copied: "已复制", unauthorized: "请先登录查看该上传批次。", forbidden: "当前账户无权查看该上传批次。", pickupNotFound: "取件码不存在或已过期。", notFound: "上传结果不存在或已经过期。", dbError: "读取上传结果失败，请稍后重试。", generic: "无法读取上传结果。", uploadResultTitle: "上传结果", pickupResultTitle: "取件结果", uploadComplete: "上传完成", generatePickup: "生成取件码", generatedPickup: "取件码已生成", manualPickupHint: "上传已保存到你的文件库。需要分享给别人时，可以生成 24 小时取件码。", generateFailed: "生成取件码失败，请稍后重试。" },
  en: { network: "Network error. Could not load the upload result.", pickup: "Pickup code", validUntil: "valid until", valid24h: "Valid for 24 hours", batch: "Batch", open: "Open", preview: "Preview", copyPreview: "Copy preview link", copied: "Copied", unauthorized: "Please sign in to view this upload batch.", forbidden: "This account cannot view this upload batch.", pickupNotFound: "The pickup code does not exist or has expired.", notFound: "The upload result does not exist or has expired.", dbError: "Could not load the upload result. Try again later.", generic: "Could not load the upload result.", uploadResultTitle: "Upload result", pickupResultTitle: "Pickup result", uploadComplete: "Upload complete", generatePickup: "Create pickup code", generatedPickup: "Pickup code created", manualPickupHint: "Your upload is saved in your file library. Create a 24-hour pickup code when you want to share it.", generateFailed: "Could not create pickup code. Try again later." }
}[lang];
const pickupCode = normalizePickupCode(params.get("code"));
const isUploadResultPage = location.pathname.startsWith("/uploads/");
const id = params.get("upload") || location.pathname.split("/").filter(Boolean).pop();
const box = document.querySelector("#result");
const panel = document.querySelector("#result-panel");
if (isUploadResultPage) setupUploadResultPage();
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
    const shareCode = pickupCode || json.batch?.pickupCode || "";
    box.innerHTML = summaryCard(json.batch, pickupCode, json.files || []) +
      (json.files || []).map((f) => fileRow(f, shareCode)).join("");
    box.querySelectorAll("[data-copy-preview]").forEach((button) => {
      button.addEventListener("click", async () => {
        await navigator.clipboard?.writeText(button.dataset.copyPreview || "");
        button.textContent = L.copied;
      });
    });
    box.querySelector("[data-generate-pickup]")?.addEventListener("click", async (event) => {
      event.currentTarget.disabled = true;
      const fileIds = (json.files || []).map((file) => file.id).filter(Boolean);
      try {
        const share = await createPickup(fileIds);
        box.innerHTML = summaryCard({ ...json.batch, pickupCode: share.pickupCode, pickupExpiresAt: share.pickupExpiresAt }, share.pickupCode, json.files || []) +
          (json.files || []).map((f) => fileRow(f, share.pickupCode)).join("");
      } catch {
        event.currentTarget.disabled = false;
        event.currentTarget.closest(".result-summary")?.insertAdjacentHTML("afterend", `<div class="pixel-alert">${escapeHtml(L.generateFailed)}</div>`);
      }
    });
  }
} catch {
  box.innerHTML = `<div class="pixel-alert">${escapeHtml(L.network)}</div>`;
}
}

function setupUploadResultPage() {
  document.documentElement.classList.add("upload-result-only");
  document.querySelector(".home-art")?.setAttribute("hidden", "");
  document.querySelector(".pickup-panel")?.setAttribute("hidden", "");
  document.querySelector("#upload-form")?.setAttribute("hidden", "");
  document.querySelector(".home-actions")?.setAttribute("hidden", "");
  const title = document.querySelector("#result-panel [data-i18n='resultTitle']");
  if (title) {
    title.removeAttribute("data-i18n");
    title.textContent = L.uploadResultTitle;
  }
}

function summaryCard(batch, fromPickupCode, files = []) {
  const code = batch.pickupCode || fromPickupCode || "";
  const expires = batch.pickupExpiresAt ? formatTime(batch.pickupExpiresAt) : L.valid24h;
  const pickup = code
    ? `<div class="pickup-result"><span>${escapeHtml(L.pickup)}</span><strong>${escapeHtml(code)}</strong><small>${escapeHtml(expires)} ${escapeHtml(L.validUntil)}</small></div>`
    : "";
  const manual = !code && batch.ownerUserId
    ? `<div class="manual-pickup"><span>${escapeHtml(L.manualPickupHint)}</span><button class="pixel-button compact" type="button" data-generate-pickup ${files.length ? "" : "disabled"}>${escapeHtml(L.generatePickup)}</button></div>`
    : "";
  return `<div class="pixel-alert ok result-summary">
    <div><strong>${escapeHtml(L.batch)} ${escapeHtml(batch.id)}</strong><br><span>${escapeHtml(statusLabel(batch.status))}</span></div>
    ${pickup}
    ${manual}
  </div>`;
}

function statusLabel(status) {
  if (status === "completed") return L.uploadComplete;
  return status || L.uploadComplete;
}

async function createPickup(fileIds) {
  const res = await fetch("/api/files/batch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "share", fileIds })
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok || !json.ok) throw new Error(json.error || L.generateFailed);
  return json.share || {};
}

function fileRow(file, code) {
  const previewURL = code ? pickupFilePath(code, file) : publicFilePath(file);
  return `<div class="file-row result-file">
    <div><strong>${escapeHtml(file.originalName)}</strong><br><span class="muted">${escapeHtml(file.mime)} · ${formatSize(file.size)}</span></div>
    <div class="result-actions">
      <button class="pixel-button secondary compact" type="button" data-copy-preview="${escapeAttr(new URL(previewURL, location.origin).href)}">${escapeHtml(L.copyPreview)}</button>
      <a class="pixel-button secondary compact" href="${escapeAttr(previewURL)}" target="_blank">${escapeHtml(L.open)}</a>
    </div>
  </div>`;
}

function publicFilePath(file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/files/${file.id}${ext}`;
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
