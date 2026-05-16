const lang = (navigator.language || "zh-CN").toLowerCase().startsWith("zh") ? "zh-CN" : "en";
const dict = {
  "zh-CN": {
    title: { home: "", login: "登录", dashboard: "控制台" },
    description: "安全上传文件，并通过 24 小时取件码进行临时分享。",
    skipContent: "跳到主内容",
    pickupTitle: "取件码",
    pickupDesc: "输入对方给你的 24 小时取件码，直接打开本次上传的文件。",
    pickupPlaceholder: "例如 8F7K2QMA",
    pickupButton: "取件",
    uploadTitle: "上传文件",
    uploadChoose: "选择或拖放文件",
    uploadDesc: "上传完成后会生成 24 小时取件码，可发给对方快捷获取。",
    uploadButton: "上传文件",
    accountLogin: "统一账户登录",
    resultTitle: "取件结果",
    movingFiles: "黑熊正在搬运文件",
    waitingUpload: "等待上传",
    cancelUpload: "取消上传",
    loginKicker: "身份验证",
    loginBubble: "黑熊会把账户授权安全带回文件服务。",
    loginTitle: "登录",
    loginDesc: "登录成功后将进入文件控制台，可管理文件、建立分享取件码和查看策略。",
    loginButton: "打开登录",
    loginError: "登录未完成，请重试。",
    loginRetry: "登录未完成，请重新尝试。",
    dashboardKicker: "控制台",
    dashboardTitle: "文件控制台",
    fileBadge: "文件"
    , uploadCancelled: "上传已取消。", pickupRequired: "请输入取件码。", pickupChecking: "正在校验取件码。", pickupMissing: "取件码不存在或已过期。", networkError: "网络连接异常，请稍后重试。", chooseFile: "请先选择文件。", uploadTooLarge: "文件超过当前服务端允许的上传限制。", uploading: "正在上传，请不要关闭页面。", uploadPrepare: "准备上传", uploadDone: "上传完成，正在打开结果页", uploadFailed: "上传失败", uploadWaitingServer: "正在上传，等待服务端确认…", uploadedPct: "已上传 {pct}%", authNeeded: "请先使用登录后再上传。", fileRequired: "没有收到文件，请重新选择。", mimeNotAllowed: "当前文件类型不允许上传。", storageFailed: "存储通道暂时不可用，请稍后重试。", dbFailed: "文件记录保存失败，请稍后重试。", uploadFailedGeneric: "上传失败，请稍后重试。"
  },
  en: {
    title: { home: "", login: "Sign in", dashboard: "Dashboard" },
    description: "Upload files securely and share them with 24-hour pickup codes.",
    skipContent: "Skip to content",
    pickupTitle: "Pickup code",
    pickupDesc: "Enter a 24-hour pickup code to open the shared files.",
    pickupPlaceholder: "Example 8F7K2QMA",
    pickupButton: "Open",
    uploadTitle: "Upload files",
    uploadChoose: "Choose or drop files",
    uploadDesc: "A 24-hour pickup code is created after upload for quick sharing.",
    uploadButton: "Upload files",
    accountLogin: "Account sign-in",
    resultTitle: "Pickup result",
    movingFiles: "The bear is moving your files",
    waitingUpload: "Waiting to upload",
    cancelUpload: "Cancel upload",
    loginKicker: "Identity",
    loginBubble: "The bear brings account authorization safely back to the file service.",
    loginTitle: "Sign in",
    loginDesc: "After signing in, you can manage files, create pickup codes, and review policies.",
    loginButton: "Open sign-in",
    loginError: "Sign-in was not completed. Try again.",
    loginRetry: "Sign-in was not completed. Try again.",
    dashboardKicker: "Dashboard",
    dashboardTitle: "File dashboard",
    fileBadge: "FILE"
    , uploadCancelled: "Upload cancelled.", pickupRequired: "Enter a pickup code.", pickupChecking: "Checking pickup code.", pickupMissing: "The pickup code does not exist or has expired.", networkError: "Network error. Try again later.", chooseFile: "Choose a file first.", uploadTooLarge: "A file exceeds the server upload limit.", uploading: "Uploading. Keep this page open.", uploadPrepare: "Preparing upload", uploadDone: "Upload complete. Opening result.", uploadFailed: "Upload failed", uploadWaitingServer: "Uploading, waiting for server confirmation...", uploadedPct: "Uploaded {pct}%", authNeeded: "Please sign in before uploading.", fileRequired: "No file was received. Choose again.", mimeNotAllowed: "This file type is not allowed.", storageFailed: "Storage is temporarily unavailable. Try again later.", dbFailed: "Could not save the file record. Try again later.", uploadFailedGeneric: "Upload failed. Try again later."
  }
};

const t = dict[lang];
document.documentElement.lang = lang;

let brandName = "myfiles";
try {
  const res = await fetch("/api/bootstrap", { cache: "no-store" });
  const json = await res.json();
  brandName = json?.brand?.name || brandName;
} catch {
  // Keep the static fallback when bootstrap is not reachable.
}

const titleKey = document.body.dataset.titleKey || "home";
const suffix = t.title[titleKey];
document.title = suffix ? `${brandName} - ${suffix}` : brandName;

setMeta("description", t.description);
setMeta("application-name", brandName);
setProperty("og:site_name", brandName);
setProperty("og:title", document.title);
setProperty("og:description", t.description);
document.querySelector("[data-geo-region]")?.setAttribute("content", lang === "zh-CN" ? "CN" : "001");

document.querySelectorAll("[data-brand]").forEach((node) => { node.textContent = brandName; });
document.querySelectorAll("[data-i18n]").forEach((node) => {
  const value = t[node.dataset.i18n];
  if (value) node.textContent = value;
});
document.querySelectorAll("[data-i18n-placeholder]").forEach((node) => {
  const value = t[node.dataset.i18nPlaceholder];
  if (value) node.setAttribute("placeholder", value);
});
document.dispatchEvent(new CustomEvent("site-i18n-ready", { detail: { lang, brandName, labels: t } }));

function setMeta(name, content) {
  document.querySelector(`meta[name="${name}"]`)?.setAttribute("content", content);
}

function setProperty(property, content) {
  document.querySelector(`meta[property="${property}"]`)?.setAttribute("content", content);
}
