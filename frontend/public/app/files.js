const rows = document.querySelector("#rows");
const q = document.querySelector("#q");
const alertBox = document.querySelector("#alert");
async function load() {
  const res = await fetch("/api/files?q=" + encodeURIComponent(q.value));
  const json = await res.json().catch(() => ({}));
  if (!res.ok) { location.href = "/login"; return; }
  rows.innerHTML = (json.files || []).map(f => `
    <tr>
      <td><a href="/d/f/${f.id}">${f.originalName}</a><br><small>${f.id}<br>${f.sha256}</small></td>
      <td>${f.mime}</td>
      <td>${formatSize(f.size)}</td>
      <td>${f.createdAt}</td>
      <td><a class="pixel-button secondary" href="${f.publicUrl || publicFilePath(f)}" target="_blank">访问</a>
        <button class="pixel-button danger" data-delete="${f.id}">删除</button></td>
    </tr>`).join("");
}
function formatSize(n){ if(n>1048576) return (n/1048576).toFixed(1)+" MiB"; if(n>1024) return (n/1024).toFixed(1)+" KiB"; return n+" B"; }
function publicFilePath(file) {
  const ext = String(file.originalName || "").match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || "";
  return `/f/${file.id}${ext}`;
}
q.addEventListener("input", () => load());
rows.addEventListener("click", async (e) => {
  const id = e.target?.dataset?.delete;
  if (!id) return;
  if (!confirm("确认软删除该文件？")) return;
  const res = await fetch("/api/files/"+id, {method:"DELETE"});
  if (!res.ok) { alertBox.className="pixel-alert"; alertBox.textContent="删除失败"; return; }
  load();
});
load();
