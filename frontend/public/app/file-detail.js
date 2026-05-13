const id = location.pathname.split("/").pop();
const fileId = id.includes(".") ? id.slice(0, id.indexOf(".")) : id;
const box = document.querySelector("#detail");
const nameEl = document.querySelector("#name");
const res = await fetch("/api/files/" + fileId);
const json = await res.json().catch(() => ({}));
if (!res.ok) {
  box.innerHTML = `<div class="pixel-alert">${json.error || "文件不存在或无权访问"}</div>`;
} else {
  const f = json.file;
  const publicUrl = f.publicUrl || publicFilePath(f);
  nameEl.textContent = f.originalName;
  box.innerHTML = `
    <div class="file-row"><strong>文件名</strong><br>${f.originalName}</div>
    <div class="file-row"><strong>MIME</strong><br>${f.mime}</div>
    <div class="file-row"><strong>大小</strong><br>${f.size}</div>
    <div class="file-row"><strong>SHA256</strong><br>${f.sha256}</div>
    <div class="file-row"><strong>上传时间</strong><br>${f.createdAt}</div>
    <div class="file-row url-row"><strong>访问链接</strong><br><a href="${publicUrl}" target="_blank">${publicUrl}</a><div class="card-actions"><button class="pixel-button secondary copy-url" type="button" data-copy="${publicUrl}">复制</button><button class="pixel-button" type="button" data-download="${publicUrl}" data-name="${escapeAttr(f.originalName)}">下载</button></div><div class="progress-wrap" id="download-progress-wrap" hidden><div class="progress-bar"><span id="download-progress-bar"></span></div><div id="download-progress-text">等待下载</div></div></div>
    <div class="file-row"><strong>公开</strong><br>${f.isPublic}</div>
    <div class="file-row"><strong>需要确认</strong><br>${f.requireConfirm}</div>
    <div class="file-row"><strong>地区策略</strong><br>${f.regionPolicy}</div>
    <div class="file-row"><strong>热链策略</strong><br>${f.hotlinkPolicy}</div>`;
}

box.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  await navigator.clipboard?.writeText(button.dataset.copy || "");
  button.textContent = "已复制";
});

box.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-download]");
  if (!button) return;
  button.disabled = true;
  try {
    await downloadWithProgress(button.dataset.download || "", button.dataset.name || "download");
  } finally {
    button.disabled = false;
  }
});

function publicFilePath(file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/f/${file.id}${ext}`;
}

async function downloadWithProgress(url, filename) {
  const wrap = document.querySelector("#download-progress-wrap");
  const bar = document.querySelector("#download-progress-bar");
  const text = document.querySelector("#download-progress-text");
  wrap.hidden = false;
  bar.style.width = "0%";
  text.textContent = "正在建立下载连接…";
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok || !res.body) {
    text.textContent = "下载失败，请稍后重试。";
    return;
  }
  const total = Number(res.headers.get("content-length") || 0);
  const reader = res.body.getReader();
  const chunks = [];
  let received = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    received += value.byteLength;
    if (total > 0) {
      const pct = Math.round((received / total) * 100);
      bar.style.width = `${pct}%`;
      text.textContent = `已下载 ${pct}%`;
    } else {
      text.textContent = `已下载 ${formatSize(received)}`;
    }
  }
  bar.style.width = "100%";
  text.textContent = "下载完成";
  const blob = new Blob(chunks);
  const href = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(href);
}

function formatSize(n) {
  if (n > 1048576) return `${(n / 1048576).toFixed(1)} MiB`;
  if (n > 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${n} B`;
}

function escapeAttr(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#039;"
  }[char]));
}
