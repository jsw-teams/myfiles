const rows = document.querySelector("#rows");
const q = document.querySelector("#q");
let files = [];

async function load() {
  const res = await fetch("/api/admin/files?q=" + encodeURIComponent(q.value) + "&owner=" + encodeURIComponent(q.value));
  const json = await res.json().catch(() => ({}));
  if (!res.ok) { rows.innerHTML = `<tr><td colspan="5">${json.error || "无权访问"}</td></tr>`; return; }
  files = json.files || [];
  rows.innerHTML = files.map(f => `
    <tr>
      <td>${escapeHTML(f.originalName)}${imageMeta(f)}<br><small>${f.id}<br>${f.sha256}</small></td>
      <td>${f.ownerUserId || "anonymous"}</td>
      <td>public=${f.isPublic}<br>confirm=${f.requireConfirm}<br>${f.regionPolicy}/${f.hotlinkPolicy}</td>
      <td>${f.status}</td>
      <td>
        <a class="pixel-button secondary" href="/admin/open/${f.id}" target="_blank">打开</a>
        <button class="pixel-button secondary" data-props="${f.id}">属性</button>
        <button class="pixel-button danger" data-delete="${f.id}">删除</button>
      </td>
    </tr>`).join("");
}
q.addEventListener("input", load);
rows.addEventListener("click", async (e) => {
  const props = e.target?.dataset?.props;
  const del = e.target?.dataset?.delete;
  if (props) openProperties(props);
  if (del && confirm("确认代管软删除？")) await fetch("/api/admin/files/"+del, {method:"DELETE"});
  load();
});
load();

function openProperties(id) {
  const f = files.find((item) => item.id === id);
  if (!f) return;
  const modal = document.createElement("dialog");
  modal.className = "file-props-dialog";
  modal.innerHTML = `
    <form method="dialog" class="pixel-card grid">
      <div>
        <div class="pixel-kicker">file properties</div>
        <h2>${escapeHTML(f.originalName)}</h2>
        <p class="muted">${f.id}${imageMeta(f)}</p>
      </div>
      <div class="grid two">
        <label>公开访问
          <select name="isPublic" class="input">
            <option value="true" ${f.isPublic ? "selected" : ""}>公开</option>
            <option value="false" ${!f.isPublic ? "selected" : ""}>不公开</option>
          </select>
        </label>
        <label>访问前确认
          <select name="requireConfirm" class="input">
            <option value="false" ${!f.requireConfirm ? "selected" : ""}>不需要</option>
            <option value="true" ${f.requireConfirm ? "selected" : ""}>需要</option>
          </select>
        </label>
        <label>地区策略
          <select name="regionPolicy" class="input">
            ${option("global", "全球允许", f.regionPolicy)}
            ${option("cn_only", "仅中国大陆", f.regionPolicy)}
            ${option("non_cn_only", "仅非中国大陆", f.regionPolicy)}
            ${option("deny_all", "全部拒绝", f.regionPolicy)}
          </select>
        </label>
        <label>热链策略
          <select name="hotlinkPolicy" class="input">
            ${option("allow", "允许热链", f.hotlinkPolicy)}
            ${option("same_site", "仅同站", f.hotlinkPolicy)}
            ${option("deny", "禁止热链", f.hotlinkPolicy)}
          </select>
        </label>
        <label>文件状态
          <select name="status" class="input">
            ${option("active", "active / 可访问", f.status)}
            ${option("hidden", "hidden / 隐藏", f.status)}
          </select>
        </label>
      </div>
      <div class="card-actions">
        <button class="pixel-button" value="save">保存属性</button>
        <button class="pixel-button secondary" value="cancel">取消</button>
      </div>
    </form>`;
  document.body.appendChild(modal);
  modal.addEventListener("close", async () => {
    if (modal.returnValue === "save") {
      const data = new FormData(modal.querySelector("form"));
      await fetch("/api/admin/files/" + id, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          isPublic: data.get("isPublic") === "true",
          requireConfirm: data.get("requireConfirm") === "true",
          regionPolicy: data.get("regionPolicy"),
          hotlinkPolicy: data.get("hotlinkPolicy"),
          status: data.get("status")
        })
      });
      await load();
    }
    modal.remove();
  });
  modal.showModal();
}

function option(value, label, selected) {
  return `<option value="${value}" ${value === selected ? "selected" : ""}>${label}</option>`;
}

function imageMeta(f) {
  return f.imageWidth && f.imageHeight ? `<br><small>${f.imageWidth} x ${f.imageHeight}</small>` : "";
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#039;"
  }[char]));
}
