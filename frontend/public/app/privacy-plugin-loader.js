(() => {
  const currentScript = document.currentScript;
  const scriptBaseUrl = currentScript?.src ? new URL(".", currentScript.src) : new URL("/", window.location.href);
  const configUrl = currentScript?.dataset.config || new URL("privacy-plugins.json?v=20260524v3", scriptBaseUrl).href;
  const stylesheetUrl = currentScript?.dataset.stylesheet || new URL("privacy-plugin-banner.css?v=20260524v3", scriptBaseUrl).href;
  const storageKey = "privacy_plugins_consent_v4";
  const legacyStorageKeys = ["privacy_plugins_consent_v3"];
  const regionKey = "privacy_plugins_region_v1";
  const globalStateKey = "__jsGripePrivacyPlugins";
  const cookieMaxAge = 60 * 60 * 24 * 180;
  const consentRegionCodes = new Set([
    "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR",
    "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK",
    "SI", "ES", "SE", "IS", "LI", "NO", "GB", "UK", "CH", "BR", "CA", "CN",
    "HK", "MO", "TW", "ZA", "JP", "US"
  ]);

  const fallbackCopy = {
    "zh-CN": {
      bannerTitle: "隐私选择",
      bannerMessage: "我们只会在你同意后启用非必要分析插件。你可以仅保留最小必要项目、全部接受，或按类别管理偏好。",
      jurisdictionNotice: {
        eu: "非必要插件默认关闭，只有在同意后才会加载；拒绝和接受同样容易，并可随时撤回。",
        us: "我们尊重全局隐私控制信号，并提供退出共享、定向广告或非必要分析的选择。",
        jp: "我们会说明第三方外部传送目的、接收方和插件用途，并在启用第三方分析前提供选择。"
      },
      rejectAll: "仅最小必要",
      acceptAll: "全部接受",
      customize: "管理偏好",
      save: "保存选择",
      close: "关闭",
      preferences: "隐私偏好中心",
      settings: "隐私设置",
      language: "语言",
      required: "必要",
      optional: "可选",
      requiredServicesTitle: "必要项目",
      requiredServicesDescription: "这些项目用于提供你请求的服务、安全、会话、语言偏好或保存隐私选择，不能在此关闭。",
      gpc: "检测到浏览器全局隐私控制或 Do Not Track，非必要插件默认保持关闭；你仍可以管理偏好。",
      categories: {
        analytics: {
          title: "分析",
          description: "帮助我们了解页面访问量和站点性能，不用于广告定向。"
        }
      }
    },
    "zh-TW": {
      bannerTitle: "隱私選擇",
      bannerMessage: "我們只會在你同意後啟用非必要分析外掛。你可以僅保留最小必要項目、全部接受，或依類別管理偏好。",
      jurisdictionNotice: {
        eu: "非必要外掛預設關閉，只有在同意後才會載入；拒絕和接受同樣容易，並可隨時撤回。",
        us: "我們尊重全域隱私控制訊號，並提供退出共享、定向廣告或非必要分析的選擇。",
        jp: "我們會說明第三方外部傳送目的、接收方和外掛用途，並在啟用第三方分析前提供選擇。"
      },
      rejectAll: "僅最小必要",
      acceptAll: "全部接受",
      customize: "管理偏好",
      save: "儲存選擇",
      close: "關閉",
      preferences: "隱私偏好中心",
      settings: "隱私設定",
      language: "語言",
      required: "必要",
      optional: "可選",
      requiredServicesTitle: "必要項目",
      requiredServicesDescription: "這些項目用於提供你要求的服務、安全、工作階段、語言偏好或儲存隱私選擇，不能在此關閉。",
      gpc: "偵測到瀏覽器全域隱私控制或 Do Not Track，非必要外掛預設保持關閉；你仍可以管理偏好。",
      categories: {
        analytics: {
          title: "分析",
          description: "幫助我們了解頁面訪問量和網站效能，不用於廣告定向。"
        }
      }
    },
    en: {
      bannerTitle: "Privacy Choices",
      bannerMessage: "We enable optional analytics plugins only after your choice. You can keep only required services, accept all, or manage preferences by category.",
      jurisdictionNotice: {
        eu: "Optional plugins are off by default, load only after consent, and consent can be withdrawn as easily as it is given.",
        us: "We honor global privacy control signals and provide choices to opt out of sharing, targeted advertising, or optional analytics.",
        jp: "We describe external transmission purposes, recipients, and plugin use before enabling third-party analytics."
      },
      rejectAll: "Only necessary",
      acceptAll: "Accept all",
      customize: "Customize",
      save: "Save choices",
      close: "Close",
      preferences: "Privacy Preference Center",
      settings: "Privacy settings",
      language: "Language",
      required: "Required",
      optional: "Optional",
      requiredServicesTitle: "Required services",
      requiredServicesDescription: "These services provide the requested site, security, session, language preference, or privacy-choice storage and cannot be turned off here.",
      gpc: "A browser Global Privacy Control or Do Not Track signal was detected, so optional plugins start off by default; you can still manage preferences.",
      categories: {
        analytics: {
          title: "Analytics",
          description: "Helps us understand page visits and site performance. It is not used for targeted advertising."
        }
      }
    }
  };

  const criticalCss = `
.privacy-plugin-banner,.privacy-plugin-overlay,.privacy-plugin-settings{box-sizing:border-box;color:#172033;font:14px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.privacy-plugin-banner *,.privacy-plugin-overlay *,.privacy-plugin-settings *{box-sizing:border-box}
.privacy-plugin-banner{position:fixed;right:16px;bottom:var(--privacy-plugin-banner-bottom,16px);left:16px;z-index:2147483000;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;align-items:center;max-width:980px;margin:0 auto;padding:16px;border:1px solid rgba(15,23,42,.18);border-radius:8px;background:#fff;box-shadow:0 18px 44px rgba(15,23,42,.18)}
.privacy-plugin-banner-content{display:grid;gap:6px}.privacy-plugin-banner strong,.privacy-plugin-title{margin:0;font-size:16px;font-weight:750;line-height:1.25}.privacy-plugin-banner p,.privacy-plugin-banner small,.privacy-plugin-lede,.privacy-plugin-notice{margin:0}.privacy-plugin-banner small,.privacy-plugin-notice,.privacy-plugin-category small{color:#4b5563;font-size:12px}
.privacy-plugin-banner-head,.privacy-plugin-panel-head{display:flex;gap:12px;align-items:center;justify-content:space-between}
.privacy-plugin-language{display:inline-flex;gap:0;border:1px solid rgba(15,23,42,.24);border-radius:6px;overflow:hidden;background:#fff}
.privacy-plugin-language button{min-height:30px;border:0;border-radius:0;padding:0 10px;background:#fff;color:#172033;font:700 12px/1 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;cursor:pointer}.privacy-plugin-language button[aria-pressed=true]{background:#172033;color:#fff}.privacy-plugin-language button:focus-visible{outline:3px solid #93c5fd;outline-offset:2px}
.privacy-plugin-actions{display:flex;gap:8px;align-items:center;justify-content:flex-end;flex-wrap:wrap}.privacy-plugin-actions button,.privacy-plugin-settings,.privacy-plugin-panel>button[data-privacy-action=close]{min-height:40px;border:1px solid #172033;border-radius:6px;padding:0 14px;background:#fff;color:#172033;font:700 14px/1 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;cursor:pointer}.privacy-plugin-actions button[data-variant=primary]{background:#172033;color:#fff}
.privacy-plugin-actions button:disabled{cursor:not-allowed;opacity:.55}.privacy-plugin-actions button:focus-visible,.privacy-plugin-settings:focus-visible,.privacy-plugin-panel>button[data-privacy-action=close]:focus-visible,.privacy-plugin-category:focus-within{outline:3px solid #93c5fd;outline-offset:2px}
.privacy-plugin-overlay{position:fixed;inset:0;z-index:2147483001;display:grid;place-items:center;padding:18px;background:rgba(15,23,42,.46)}.privacy-plugin-panel{display:grid;gap:14px;width:min(760px,100%);max-height:min(720px,calc(100vh - 36px));overflow:auto;padding:20px;border-radius:8px;background:#fff;box-shadow:0 24px 70px rgba(15,23,42,.32)}
.privacy-plugin-categories{display:grid;gap:10px}.privacy-plugin-category{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:12px;align-items:start;padding:14px;border:1px solid rgba(15,23,42,.14);border-radius:8px;cursor:pointer}.privacy-plugin-category input{width:20px;height:20px;margin:2px 0 0;accent-color:#172033}.privacy-plugin-category span{display:grid;gap:4px}.privacy-plugin-category strong{font-size:15px}.privacy-plugin-category em{color:#4b5563;font-style:normal;font-weight:700}
.privacy-plugin-sr-only{position:absolute;width:1px;height:1px;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.privacy-plugin-settings{position:fixed;right:16px;bottom:var(--privacy-plugin-settings-bottom,var(--privacy-plugin-banner-bottom,16px));z-index:2147482999;min-height:36px;background:#fff;box-shadow:0 12px 30px rgba(15,23,42,.16)}.privacy-plugin-panel>button[data-privacy-action=close]{justify-self:start;border-color:transparent;padding-inline:0;color:#4b5563}
@media (max-width:720px){.privacy-plugin-banner{grid-template-columns:1fr}.privacy-plugin-actions{justify-content:stretch}.privacy-plugin-actions button{flex:1 1 120px}.privacy-plugin-banner-head,.privacy-plugin-panel-head{align-items:flex-start;flex-direction:column}.privacy-plugin-category{grid-template-columns:auto minmax(0,1fr)}.privacy-plugin-category em{grid-column:2}}
`;

  const normalizeCountry = (value) => String(value || "").trim().toUpperCase();
  const normalizeLang = (value) => {
    const lang = String(value || "").trim().toLowerCase();
    if (!lang) return "";
    if (lang === "zh-tw" || lang === "zh-hk" || lang === "zh-mo" || lang.startsWith("zh-hant")) return "zh-TW";
    if (lang === "zh-cn" || lang === "zh-sg" || lang.startsWith("zh-hans") || lang === "zh") return "zh-CN";
    if (lang.startsWith("en")) return "en";
    return "";
  };
  const siteLang = () => {
    const scriptedLang = normalizeLang(currentScript?.dataset.lang);
    if (scriptedLang) return scriptedLang;
    const htmlLang = normalizeLang(document.documentElement.getAttribute("lang"));
    if (htmlLang) return htmlLang;
    const pathLang = normalizeLang(window.location.pathname.split("/").filter(Boolean)[0]);
    if (pathLang) return pathLang;
    const currentLocale = document.querySelector("[data-locale-choice][aria-current='page']")?.getAttribute("data-locale-choice");
    return normalizeLang(currentLocale);
  };
  const browserLang = () => {
    const pageLang = siteLang();
    if (pageLang) return pageLang;
    const languages = navigator.languages?.length ? navigator.languages : [navigator.language || "en"];
    return normalizeLang(languages[0]) || "en";
  };
  let activeLang = browserLang();

  function cookieAttributes(maxAge = cookieMaxAge) {
    const secure = window.location.protocol === "https:" ? "; secure" : "";
    return `; path=/; max-age=${maxAge}; samesite=lax${secure}`;
  }

  function readCookie(name) {
    const encodedName = `${encodeURIComponent(name)}=`;
    const match = document.cookie
      .split(";")
      .map((item) => item.trim())
      .find((item) => item.startsWith(encodedName));
    if (!match) return "";
    try {
      return decodeURIComponent(match.slice(encodedName.length));
    } catch {
      return "";
    }
  }

  function writeCookie(name, value) {
    document.cookie = `${encodeURIComponent(name)}=${encodeURIComponent(value)}${cookieAttributes()}`;
  }

  function deleteCookie(name) {
    document.cookie = `${encodeURIComponent(name)}=${cookieAttributes(0)}`;
  }

  function readLocalJson(key) {
    try {
      return JSON.parse(localStorage.getItem(key) || "null");
    } catch {
      return null;
    }
  }

  function removeLocalJson(key) {
    try {
      localStorage.removeItem(key);
    } catch {
      /* Ignore private-mode storage errors. */
    }
  }

  function readJson(key) {
    const cookieValue = readCookie(key);
    if (cookieValue) {
      try {
        return JSON.parse(cookieValue);
      } catch {
        deleteCookie(key);
        return null;
      }
    }

    const legacyValue = readLocalJson(key);
    if (legacyValue) {
      writeJson(key, legacyValue);
      removeLocalJson(key);
    }
    return legacyValue;
  }

  function writeJson(key, value) {
    try {
      writeCookie(key, JSON.stringify(value));
    } catch {
      /* Ignore cookie serialization errors. */
    }
    removeLocalJson(key);
  }

  function removeJson(key) {
    deleteCookie(key);
    removeLocalJson(key);
  }

  function hasGlobalOptOut() {
    return navigator.globalPrivacyControl === true || navigator.doNotTrack === "1" || window.doNotTrack === "1";
  }

  function loadStylesheet() {
    if (!document.querySelector("style[data-privacy-plugin-critical-style]")) {
      const style = document.createElement("style");
      style.dataset.privacyPluginCriticalStyle = "true";
      style.textContent = criticalCss;
      document.head.append(style);
    }
    if (document.querySelector(`link[data-privacy-plugin-style][href="${stylesheetUrl}"]`)) return;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = stylesheetUrl;
    link.dataset.privacyPluginStyle = "banner";
    document.head.append(link);
  }

  async function detectCountry() {
    const cached = readJson(regionKey);
    if (cached?.country && Date.now() - cached.time < 86400000) return cached.country;

    try {
      const response = await fetch("/cdn-cgi/trace", {
        cache: "no-store",
        credentials: "omit"
      });
      if (response.ok) {
        const trace = await response.text();
        const match = trace.match(/^loc=([A-Z]{2})$/m);
        if (match) {
          const country = normalizeCountry(match[1]);
          writeJson(regionKey, { country, time: Date.now() });
          return country;
        }
      }
    } catch {
      /* Cloudflare trace is unavailable in local previews or non-Cloudflare hosts. */
    }

    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
    if (/Europe\//.test(timezone)) return "EU";
    if (/Asia\/Tokyo/.test(timezone)) return "JP";
    if (/America\//.test(timezone)) return "US";
    if (/Asia\/(Shanghai|Hong_Kong|Macau|Taipei)/.test(timezone)) return "CN";
    return "UNKNOWN";
  }

  function regionProfile(country) {
    if (country === "EU" || ["GB", "UK", "CH", "IS", "LI", "NO"].includes(country) || consentRegionCodes.has(country) && !["US", "JP", "CN", "HK", "MO", "TW", "BR", "CA", "ZA"].includes(country)) return "eu";
    if (country === "US") return "us";
    if (country === "JP") return "jp";
    return "default";
  }

  function categoryOf(plugin) {
    return plugin.policy?.category || "analytics";
  }

  function modeFor(plugin, config) {
    return plugin.policy?.consentMode || config.consent?.mode || plugin.policy?.mode || "region-aware";
  }

  function needsConsent(country, plugin, config) {
    const mode = modeFor(plugin, config);
    if (mode === "always-prompt") return true;
    if (mode === "never-prompt") return false;
    const explicitCountries = new Set((plugin.policy?.consentCountries || []).map(normalizeCountry));
    if (explicitCountries.has(country)) return true;
    if (country === "EU" || country === "UNKNOWN") return true;
    return mode === "consent" || consentRegionCodes.has(country);
  }

  function loadPlugin(plugin) {
    if ([...document.scripts].some((script) => script.dataset.privacyPlugin === plugin.id)) return;
    if (plugin.type !== "script" || !plugin.src) return;

    const script = document.createElement("script");
    script.src = plugin.src;
    script.dataset.privacyPlugin = plugin.id;
    Object.entries(plugin.attributes || {}).forEach(([key, value]) => {
      if (value === false || value == null) return;
      if (value === true) {
        script.setAttribute(key, "");
      } else {
        script.setAttribute(key, String(value));
      }
    });
    document.head.append(script);
  }

  function text(tag, value, className) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    return node;
  }

  function button(label, action, variant = "secondary") {
    const node = document.createElement("button");
    node.type = "button";
    node.dataset.privacyAction = action;
    node.dataset.variant = variant;
    node.textContent = label;
    return node;
  }

  function languageSwitcher(config, onChange) {
    const copy = copyFor(config);
    const switcher = document.createElement("div");
    switcher.className = "privacy-plugin-language";
    switcher.setAttribute("role", "group");
    switcher.setAttribute("aria-label", copy.language || "Language");
    [
      ["zh-CN", "简"],
      ["zh-TW", "繁"],
      ["en", "EN"]
    ].forEach(([lang, label]) => {
      const node = document.createElement("button");
      node.type = "button";
      node.textContent = label;
      node.setAttribute("aria-pressed", String((activeLang || browserLang()) === lang));
      node.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        activeLang = lang;
        onChange();
      });
      switcher.append(node);
    });
    return switcher;
  }

  function copyFor(config) {
    const lang = activeLang || browserLang();
    return config.ui?.[lang] || config.ui?.en || fallbackCopy[lang] || fallbackCopy.en;
  }

  function localizedValue(value) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return value;
    const lang = activeLang || browserLang();
    return value[lang] || value.en || value["zh-CN"] || Object.values(value).find(Boolean) || "";
  }

  function categoryCopy(copy, category) {
    return copy.categories?.[category] || fallbackCopy.en.categories[category] || {
      title: category,
      description: ""
    };
  }

  function saveConsent(status, choices, country) {
    const value = {
      status,
      choices,
      country,
      updatedAt: new Date().toISOString(),
      version: 4
    };
    writeJson(storageKey, value);
    legacyStorageKeys.forEach(removeJson);
    return value;
  }

  function categoriesFor(context) {
    return [...new Set(context.gatedPlugins.map(categoryOf))];
  }

  function emptyChoices(context) {
    return Object.fromEntries(categoriesFor(context).map((category) => [category, false]));
  }

  function readSavedConsent(context) {
    const saved = readJson(storageKey);
    if (saved?.version === 4 && saved.status) return saved;

    const legacy = legacyStorageKeys.map(readJson).find((value) => value?.status);
    if (!legacy) return null;
    const choices = { ...emptyChoices(context), ...(legacy.choices || {}) };
    const rejectsAll = legacy.status === "rejected" || categoriesFor(context).every((category) => choices[category] !== true);
    if (!rejectsAll) return null;
    return saveConsent("rejected", emptyChoices(context), context.country);
  }

  function requiredServicesFor(config) {
    return Array.isArray(config.requiredServices) ? config.requiredServices.filter(Boolean) : [];
  }

  function applyConsent(plugins, gatedPlugins, consent) {
    const choices = consent?.choices || {};
    const gatedIds = new Set(gatedPlugins.map((plugin) => plugin.id));
    plugins.forEach((plugin) => {
      if (!gatedIds.has(plugin.id)) {
        loadPlugin(plugin);
        return;
      }
      if (choices[categoryOf(plugin)] === true) loadPlugin(plugin);
    });
  }

  function needsReloadForDisabledPlugins(gatedPlugins, choices) {
    return gatedPlugins.some((plugin) => {
      if (choices[categoryOf(plugin)] === true) return false;
      return Boolean(document.querySelector(`script[data-privacy-plugin="${plugin.id}"]`));
    });
  }

  function renderSettingsButton(config, context) {
    document.querySelectorAll("[data-privacy-settings]").forEach((node) => node.remove());
    loadStylesheet();
    const copy = copyFor(config);
    const trigger = button(copy.settings, "settings", "secondary");
    trigger.className = "privacy-plugin-settings";
    trigger.dataset.privacySettings = "true";
    trigger.setAttribute("aria-haspopup", "dialog");
    trigger.addEventListener("click", () => {
      const latest = window[globalStateKey] || { config, context };
      if (latest.config && latest.context) showPreferenceCenter(latest.config, latest.context);
    });
    document.body.append(trigger);
  }

  function showPreferenceCenter(config, context) {
    loadStylesheet();
    document.querySelectorAll("[data-privacy-root]").forEach((node) => node.remove());
    const copy = copyFor(config);
    const saved = readSavedConsent(context);
    const choices = { ...(saved?.choices || {}) };
    const categories = categoriesFor(context);
    categories.forEach((category) => {
      if (choices[category] == null) choices[category] = false;
    });
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const titleId = "privacy-plugin-title";
    const descriptionId = "privacy-plugin-description";

    const overlay = document.createElement("section");
    overlay.className = "privacy-plugin-overlay";
    overlay.dataset.privacyRoot = "true";
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-modal", "true");
    overlay.setAttribute("aria-labelledby", titleId);
    overlay.setAttribute("aria-describedby", descriptionId);

    const panel = document.createElement("div");
    panel.className = "privacy-plugin-panel";
    const header = document.createElement("div");
    header.className = "privacy-plugin-panel-head";
    const title = text("h2", copy.preferences, "privacy-plugin-title");
    title.id = titleId;
    header.append(title, languageSwitcher(config, () => {
      overlay.remove();
      showPreferenceCenter(config, context);
    }));
    panel.append(header);
    const lede = text("p", context.globalOptOut ? copy.gpc : copy.bannerMessage, "privacy-plugin-lede");
    lede.id = descriptionId;
    panel.append(lede);
    const profile = regionProfile(context.country);
    const notice = copy.jurisdictionNotice?.[profile] || copy.jurisdictionNotice?.default;
    if (notice) panel.append(text("p", notice, "privacy-plugin-notice"));

    const list = document.createElement("div");
    list.className = "privacy-plugin-categories";
    const requiredServices = requiredServicesFor(config);
    if (requiredServices.length) {
      requiredServices.forEach((service) => {
        const item = document.createElement("label");
        item.className = "privacy-plugin-category";
        const input = document.createElement("input");
        input.type = "checkbox";
        input.checked = true;
        input.disabled = true;
        input.setAttribute("aria-label", service.name || service.id || copy.required);
        const body = document.createElement("span");
        body.append(text("strong", service.name || service.id || copy.requiredServicesTitle || copy.required));
        body.append(text("span", localizedValue(service.disclosure) || copy.requiredServicesDescription || ""));
        item.append(input, body, text("em", copy.required));
        list.append(item);
      });
    }
    categories.forEach((category) => {
      const categoryPlugins = context.gatedPlugins.filter((plugin) => categoryOf(plugin) === category);
      const categoryInfo = categoryCopy(copy, category);
      const item = document.createElement("label");
      item.className = "privacy-plugin-category";
      const input = document.createElement("input");
      input.type = "checkbox";
      input.dataset.category = category;
      input.checked = choices[category] === true;
      input.addEventListener("change", () => {
        choices[category] = input.checked;
      });
      const body = document.createElement("span");
      body.append(text("strong", categoryInfo.title));
      body.append(text("span", categoryInfo.description));
      categoryPlugins.forEach((plugin) => {
        body.append(text("small", localizedValue(plugin.disclosure) || plugin.name || plugin.id));
      });
      item.append(input, body, text("em", copy.optional));
      list.append(item);
    });
    panel.append(list);

    const actions = document.createElement("div");
    actions.className = "privacy-plugin-actions";
    actions.append(
      button(copy.rejectAll, "reject", "secondary"),
      button(copy.save, "save", "primary"),
      button(copy.acceptAll, "accept", "secondary")
    );
    panel.append(actions);
    panel.append(button(copy.close, "close", "text"));
    overlay.append(panel);
    document.body.append(overlay);
    panel.tabIndex = -1;
    const focusTarget = panel;
    focusTarget.focus?.();

    const closePreferenceCenter = () => {
      overlay.remove();
      previousFocus?.focus?.();
      if (!readSavedConsent(context)) showBanner(config, context);
    };

    overlay.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closePreferenceCenter();
      }
    });

    overlay.addEventListener("click", (event) => {
      const action = event.target.closest("[data-privacy-action]")?.dataset.privacyAction;
      if (!action) return;
      event.preventDefault();
      event.stopPropagation();
      if (action === "close") {
        closePreferenceCenter();
        return;
      }
      if (action === "reject" || action === "accept") {
        list.querySelectorAll("input[type='checkbox']").forEach((input) => {
          input.checked = action === "accept";
        });
      }
      list.querySelectorAll("input[type='checkbox']").forEach((input) => {
        if (!input.disabled) choices[input.dataset.category] = input.checked;
      });
      const consent = saveConsent(action === "accept" ? "accepted" : action === "reject" ? "rejected" : "custom", choices, context.country);
      overlay.remove();
      if (needsReloadForDisabledPlugins(context.gatedPlugins, choices)) {
        window.location.reload();
        return;
      }
      applyConsent(context.plugins, context.gatedPlugins, consent);
      renderSettingsButton(config, context);
    });
  }

  function showBanner(config, context) {
    loadStylesheet();
    document.querySelectorAll("[data-privacy-root]").forEach((node) => node.remove());
    const copy = copyFor(config);
    const banner = document.createElement("section");
    banner.className = "privacy-plugin-banner";
    banner.dataset.privacyRoot = "true";
    banner.setAttribute("role", "dialog");
    banner.setAttribute("aria-live", "polite");
    banner.setAttribute("aria-labelledby", "privacy-plugin-banner-title");
    banner.setAttribute("aria-describedby", "privacy-plugin-banner-description");

    const content = document.createElement("div");
    content.className = "privacy-plugin-banner-content";
    const head = document.createElement("div");
    head.className = "privacy-plugin-banner-head";
    const title = text("strong", copy.bannerTitle);
    title.id = "privacy-plugin-banner-title";
    head.append(title, languageSwitcher(config, () => {
      banner.remove();
      showBanner(config, context);
    }));
    content.append(head);
    const description = text("p", context.globalOptOut ? copy.gpc : copy.bannerMessage);
    description.id = "privacy-plugin-banner-description";
    content.append(description);
    const profile = regionProfile(context.country);
    const notice = copy.jurisdictionNotice?.[profile] || copy.jurisdictionNotice?.default;
    if (notice) content.append(text("small", notice));

    const actions = document.createElement("div");
    actions.className = "privacy-plugin-actions";
    actions.append(
      button(copy.rejectAll, "reject", "secondary"),
      button(copy.customize, "customize", "secondary"),
      button(copy.acceptAll, "accept", "primary")
    );
    banner.append(content, actions);
    document.body.append(banner);

    banner.addEventListener("click", (event) => {
      const action = event.target.closest("[data-privacy-action]")?.dataset.privacyAction;
      if (!action) return;
      event.preventDefault();
      event.stopPropagation();
      if (action === "customize") {
        showPreferenceCenter(config, context);
        return;
      }
      const categories = categoriesFor(context);
      const choices = {};
      categories.forEach((category) => {
        choices[category] = action === "accept";
      });
      const consent = saveConsent(action === "accept" ? "accepted" : "rejected", choices, context.country);
      banner.remove();
      if (needsReloadForDisabledPlugins(context.gatedPlugins, choices)) {
        window.location.reload();
        return;
      }
      applyConsent(context.plugins, context.gatedPlugins, consent);
      renderSettingsButton(config, context);
    });
  }

  async function boot() {
    const config = await fetch(configUrl, { cache: "no-store", credentials: "same-origin" }).then((response) => response.json());
    const plugins = (config.plugins || []).filter((plugin) => plugin && plugin.enabled !== false);
    if (!plugins.length) return;

    const country = await detectCountry();
    const globalOptOut = hasGlobalOptOut();
    const gatedPlugins = plugins.filter((plugin) => needsConsent(country, plugin, config));
    const context = { plugins, gatedPlugins, country, globalOptOut };
    window[globalStateKey] = { config, context };
    window.JSGripePrivacy = {
      openPreferences: () => showPreferenceCenter(config, context),
      reset: () => {
        [storageKey, ...legacyStorageKeys].forEach(removeJson);
        document.querySelectorAll("[data-privacy-root],[data-privacy-settings]").forEach((node) => node.remove());
        showBanner(config, context);
      }
    };
    let saved = readSavedConsent(context);

    if (!gatedPlugins.length) {
      if (!globalOptOut) plugins.forEach(loadPlugin);
      return;
    }
    if (saved?.version === 4) {
      applyConsent(plugins, gatedPlugins, saved);
      renderSettingsButton(config, context);
      return;
    }
    showBanner(config, context);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => boot().catch(() => {}), { once: true });
  } else {
    boot().catch(() => {});
  }
})();
