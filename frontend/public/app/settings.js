const form = document.querySelector("#settings-form");
const alertBox = document.querySelector("#alert");
const reloadButton = document.querySelector("#reload");

const defaults = {
  "site.brandName": "myfiles",
  "site.baseUrl": "https://files.js.gripe",
  "site.supportEmail": "helper@js.gripe",
  "site.notice": "",
  "seo.publicFilesIndex": "policy",

  "upload.maxMB": 100,
  "upload.allowAnonymous": true,
  "upload.mimeMode": "all",
  "upload.allowedMimeTypes": "*/*",
  "upload.maxFilesPerBatch": 20,

  "file.defaultPublic": true,
  "file.defaultRequireConfirm": false,
  "file.defaultRegionPolicy": "global",
  "file.defaultHotlinkPolicy": "allow",

  "storage.mode": "tgbots",
  "storage.uploadUrl": "https://gateway.js.gripe/api/v1/tgbots",
  "storage.chatId": "",
  "storage.timeoutSeconds": 120,
  "storage.localDir": "/var/lib/myfiles/storage",
  "storage.apiKey": "",

  "cdn.enabled": true,
  "cdn.baseUrl": "https://files.js.gripe",
  "cdn.publicCacheControl": "public, max-age=31536000, immutable",
  "cdn.privateCacheControl": "no-store",

  "security.sessionCookieName": "myfiles_session",
  "security.sessionTtlHours": 168,
  "security.cookieSecure": true,
  "audit.retentionDays": 180
};

let currentValues = { ...defaults };
let saving = false;

await loadSettings();
installSectionSaves();

reloadButton?.addEventListener("click", loadSettings);

form?.addEventListener("input", () => {
  currentValues = { ...currentValues, ...collectForm({ includeEmptySecret: false }) };
});

form?.addEventListener("submit", async (event) => {
  event.preventDefault();
});

async function loadSettings() {
  try {
    showAlert("正在读取设置…", false);

    const res = await fetch("/api/admin/settings", {
      credentials: "include",
      cache: "no-store"
    });

    const json = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(json.error || "无法读取设置");
    }

    const loaded = flattenSettings(json.settings || {});
    currentValues = { ...defaults, ...loaded };

    fillForm(currentValues);
    showAlert("设置已读取", true);
  } catch (error) {
    showAlert(error.message || "读取设置失败", false);
  }
}

async function saveSettings(scope) {
  if (saving) {
    showAlert("正在保存当前板块，请稍候…", false);
    return;
  }
  const button = scope?.querySelector("[data-save-section]");
  try {
    saving = true;
    setSavingState(button, true);
    const body = collectForm({ includeEmptySecret: true, scope });

    if (!body["storage.apiKey"]) {
      delete body["storage.apiKey"];
    }

    validateSettings(body);

    if (containsStorageSettings(body) && body["storage.mode"] === "tgbots") {
      if (!body["storage.apiKey"] && !currentValues["storage.apiKeyConfigured"]) {
        throw new Error("首次启用 tgbots 存储通道时，请填写存储 API Key。");
      }
      showAlert("正在测试 Telegram 存储通道，测试通过后才会保存…", false, scope);
    }

    const res = await fetch("/api/admin/settings", {
      method: "PATCH",
      credentials: "include",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify(body)
    });

    const json = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(json.error || "保存失败");
    }

    const loaded = flattenSettings(json.settings || {});
    currentValues = { ...defaults, ...loaded };

    fillForm(currentValues, { keepSecretBlank: true });
    showAlert("当前板块已保存", true, scope);
  } catch (error) {
    showAlert(error.message || "保存失败", false, scope);
  } finally {
    saving = false;
    setSavingState(button, false);
  }
}

function containsStorageSettings(body) {
  return Object.keys(body).some((key) => key.startsWith("storage."));
}

function installSectionSaves() {
  document.querySelectorAll(".settings-section").forEach((section) => {
    if (!section.querySelector("[name]")) {
      return;
    }
    const title = section.querySelector("h2")?.textContent?.trim() || "当前板块";
    const row = document.createElement("div");
    row.className = "section-save-row";
    row.innerHTML = `<button type="button" class="pixel-button secondary" data-save-section>保存${title}</button>`;
    section.appendChild(row);
    row.querySelector("button").addEventListener("click", () => saveSettings(section));
  });
}

function setSavingState(activeButton, isSaving) {
  if (activeButton && !activeButton.dataset.label) {
    activeButton.dataset.label = activeButton.textContent;
  }
  document.querySelectorAll("[data-save-section]").forEach((button) => {
    button.disabled = isSaving;
  });
  if (activeButton) {
    activeButton.textContent = isSaving ? "保存中…" : activeButton.dataset.label;
  }
}

function flattenSettings(settings) {
  const result = {};
  for (const [key, entry] of Object.entries(settings)) {
    if (entry && typeof entry === "object" && "value" in entry) {
      result[key] = entry.value;
    } else {
      result[key] = entry;
    }
  }
  return result;
}

function fillForm(values, options = {}) {
  for (const element of form.elements) {
    if (!element.name) continue;

    if (element.name === "storage.apiKey" && options.keepSecretBlank) {
      element.value = "";
      continue;
    }

    const value = values[element.name];
    if (value === undefined || value === null) continue;

    if (element.tagName === "SELECT") {
      element.value = String(value);
      continue;
    }

    if (element.type === "number") {
      element.value = Number(value);
      continue;
    }

    if (element.name === "upload.allowedMimeTypes") {
      element.value = Array.isArray(value) ? value.join("\n") : String(value);
      continue;
    }

    element.value = String(value);
  }
}

function collectForm({ includeEmptySecret, scope } = {}) {
  const data = {};
  const fd = new FormData(form);
  const allowedNames = scope
    ? new Set([...scope.querySelectorAll("[name]")].map((item) => item.name))
    : null;

  for (const [key, raw] of fd.entries()) {
    if (allowedNames && !allowedNames.has(key)) {
      continue;
    }

    if (key === "storage.apiKey" && !includeEmptySecret && !String(raw || "").trim()) {
      continue;
    }

    data[key] = normalizeValue(key, raw);
  }

  return data;
}

function normalizeValue(key, raw) {
  const value = String(raw ?? "").trim();

  if ([
    "upload.allowAnonymous",
    "file.defaultPublic",
    "file.defaultRequireConfirm",
    "cdn.enabled",
    "security.cookieSecure"
  ].includes(key)) {
    return value === "true";
  }

  if ([
    "upload.maxMB",
    "upload.maxFilesPerBatch",
    "storage.timeoutSeconds",
    "security.sessionTtlHours",
    "audit.retentionDays"
  ].includes(key)) {
    return Number(value || 0);
  }

  if (key === "upload.allowedMimeTypes") {
    const list = value
      .split(/\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);
    return list.length ? list : ["*/*"];
  }

  return value;
}

function validateSettings(data) {
  if ("site.baseUrl" in data && !data["site.baseUrl"]?.startsWith("https://")) {
    throw new Error("公开域名必须以 https:// 开头");
  }

  if ("cdn.baseUrl" in data && data["cdn.baseUrl"] && !String(data["cdn.baseUrl"]).startsWith("https://")) {
    throw new Error("CDN Base URL 必须以 https:// 开头");
  }

  if ("upload.maxMB" in data && data["upload.maxMB"] < 1) {
    throw new Error("上传上限必须大于 0 MB");
  }

  if ("upload.maxFilesPerBatch" in data && data["upload.maxFilesPerBatch"] < 1) {
    throw new Error("批次最大文件数必须大于 0");
  }

  if ("storage.timeoutSeconds" in data && data["storage.timeoutSeconds"] < 5) {
    throw new Error("存储超时不能小于 5 秒");
  }

  if ("security.sessionTtlHours" in data && data["security.sessionTtlHours"] < 1) {
    throw new Error("Session 有效期不能小于 1 小时");
  }
}

function showAlert(message, ok, scope) {
  alertBox.hidden = false;
  alertBox.className = ok ? "settings-alert ok" : "settings-alert";
  alertBox.textContent = message;
  if (!scope) {
    return;
  }
  let local = scope.querySelector(".section-alert");
  if (!local) {
    local = document.createElement("div");
    local.className = "section-alert";
    scope.appendChild(local);
  }
  local.className = ok ? "section-alert ok" : "section-alert";
  local.textContent = message;
  local.hidden = false;
}
