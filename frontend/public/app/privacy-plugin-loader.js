(() => {
  const currentScript = document.currentScript;
  const configUrl = currentScript?.dataset.config || "/privacy-plugins.json";
  const stylesheetUrl = currentScript?.dataset.stylesheet || "/privacy-plugin-banner.css";
  const storageKey = "privacy_plugins_consent_v3";
  const regionKey = "privacy_plugins_region_v1";
  const consentRegionCodes = new Set([
    "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR",
    "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK",
    "SI", "ES", "SE", "IS", "LI", "NO", "GB", "UK", "CH", "BR", "CA", "CN",
    "HK", "MO", "TW", "ZA", "JP", "US"
  ]);

  const fallbackCopy = {
    "zh-CN": {
      bannerTitle: "隐私选择",
      bannerMessage: "我们只会在你同意后启用非必要分析插件。你可以拒绝、接受，或按类别管理偏好。",
      jurisdictionNotice: {
        eu: "非必要插件默认关闭，只有在同意后才会加载；拒绝和接受同样容易，并可随时撤回。",
        us: "我们尊重全局隐私控制信号，并提供退出共享、定向广告或非必要分析的选择。",
        jp: "我们会说明第三方外部传送目的、接收方和插件用途，并在启用第三方分析前提供选择。"
      },
      rejectAll: "全部拒绝",
      acceptAll: "全部接受",
      customize: "管理偏好",
      save: "保存选择",
      close: "关闭",
      preferences: "隐私偏好中心",
      settings: "隐私设置",
      required: "必要",
      optional: "可选",
      gpc: "检测到浏览器全局隐私控制或 Do Not Track，非必要插件已保持关闭。",
      categories: {
        analytics: {
          title: "分析",
          description: "帮助我们了解页面访问量和站点性能，不用于广告定向。"
        }
      }
    },
    "zh-TW": {
      bannerTitle: "隱私選擇",
      bannerMessage: "我們只會在你同意後啟用非必要分析外掛。你可以拒絕、接受，或依類別管理偏好。",
      jurisdictionNotice: {
        eu: "非必要外掛預設關閉，只有在同意後才會載入；拒絕和接受同樣容易，並可隨時撤回。",
        us: "我們尊重全域隱私控制訊號，並提供退出共享、定向廣告或非必要分析的選擇。",
        jp: "我們會說明第三方外部傳送目的、接收方和外掛用途，並在啟用第三方分析前提供選擇。"
      },
      rejectAll: "全部拒絕",
      acceptAll: "全部接受",
      customize: "管理偏好",
      save: "儲存選擇",
      close: "關閉",
      preferences: "隱私偏好中心",
      settings: "隱私設定",
      required: "必要",
      optional: "可選",
      gpc: "偵測到瀏覽器全域隱私控制或 Do Not Track，非必要外掛已保持關閉。",
      categories: {
        analytics: {
          title: "分析",
          description: "幫助我們了解頁面訪問量和網站效能，不用於廣告定向。"
        }
      }
    },
    en: {
      bannerTitle: "Privacy Choices",
      bannerMessage: "We enable optional analytics plugins only after your choice. You can reject, accept, or manage preferences by category.",
      jurisdictionNotice: {
        eu: "Optional plugins are off by default, load only after consent, and consent can be withdrawn as easily as it is given.",
        us: "We honor global privacy control signals and provide choices to opt out of sharing, targeted advertising, or optional analytics.",
        jp: "We describe external transmission purposes, recipients, and plugin use before enabling third-party analytics."
      },
      rejectAll: "Reject all",
      acceptAll: "Accept all",
      customize: "Customize",
      save: "Save choices",
      close: "Close",
      preferences: "Privacy Preference Center",
      settings: "Privacy settings",
      required: "Required",
      optional: "Optional",
      gpc: "A browser Global Privacy Control or Do Not Track signal was detected, so optional plugins remain off.",
      categories: {
        analytics: {
          title: "Analytics",
          description: "Helps us understand page visits and site performance. It is not used for targeted advertising."
        }
      }
    }
  };

  const normalizeCountry = (value) => String(value || "").trim().toUpperCase();
  const browserLang = () => {
    const languages = navigator.languages?.length ? navigator.languages : [navigator.language || "en"];
    const language = String(languages[0] || "en").toLowerCase();
    if (language === "zh-tw" || language === "zh-hk" || language === "zh-mo" || language.startsWith("zh-hant")) return "zh-TW";
    if (language.startsWith("zh")) return "zh-CN";
    return "en";
  };
  const readJson = (key) => {
    try {
      return JSON.parse(localStorage.getItem(key) || "null");
    } catch {
      return null;
    }
  };
  const writeJson = (key, value) => {
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      /* Ignore private-mode storage errors. */
    }
  };

  function hasGlobalOptOut() {
    return navigator.globalPrivacyControl === true || navigator.doNotTrack === "1" || window.doNotTrack === "1";
  }

  function loadStylesheet() {
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

  function copyFor(config) {
    const lang = browserLang();
    return config.ui?.[lang] || config.ui?.en || fallbackCopy[lang] || fallbackCopy.en;
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
      version: 3
    };
    writeJson(storageKey, value);
    return value;
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
    if (document.querySelector("[data-privacy-settings]")) return;
    loadStylesheet();
    const copy = copyFor(config);
    const trigger = button(copy.settings, "settings", "secondary");
    trigger.className = "privacy-plugin-settings";
    trigger.dataset.privacySettings = "true";
    trigger.addEventListener("click", () => showPreferenceCenter(config, context));
    document.body.append(trigger);
  }

  function showPreferenceCenter(config, context) {
    loadStylesheet();
    document.querySelectorAll("[data-privacy-root]").forEach((node) => node.remove());
    const copy = copyFor(config);
    const saved = readJson(storageKey);
    const choices = { ...(saved?.choices || {}) };
    const categories = [...new Set(context.gatedPlugins.map(categoryOf))];
    categories.forEach((category) => {
      if (choices[category] == null) choices[category] = false;
    });

    const overlay = document.createElement("section");
    overlay.className = "privacy-plugin-overlay";
    overlay.dataset.privacyRoot = "true";
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-modal", "true");
    overlay.setAttribute("aria-labelledby", "privacy-plugin-title");

    const panel = document.createElement("div");
    panel.className = "privacy-plugin-panel";
    const title = text("h2", copy.preferences, "privacy-plugin-title");
    title.id = "privacy-plugin-title";
    panel.append(title);
    panel.append(text("p", context.globalOptOut ? copy.gpc : copy.bannerMessage, "privacy-plugin-lede"));
    const profile = regionProfile(context.country);
    const notice = copy.jurisdictionNotice?.[profile] || copy.jurisdictionNotice?.default;
    if (notice) panel.append(text("p", notice, "privacy-plugin-notice"));

    const list = document.createElement("div");
    list.className = "privacy-plugin-categories";
    categories.forEach((category) => {
      const categoryPlugins = context.gatedPlugins.filter((plugin) => categoryOf(plugin) === category);
      const categoryInfo = categoryCopy(copy, category);
      const item = document.createElement("label");
      item.className = "privacy-plugin-category";
      const input = document.createElement("input");
      input.type = "checkbox";
      input.checked = choices[category] === true && !context.globalOptOut;
      input.disabled = context.globalOptOut;
      input.addEventListener("change", () => {
        choices[category] = input.checked;
      });
      const body = document.createElement("span");
      body.append(text("strong", categoryInfo.title));
      body.append(text("span", categoryInfo.description));
      categoryPlugins.forEach((plugin) => {
        body.append(text("small", plugin.disclosure || plugin.name || plugin.id));
      });
      item.append(input, body, text("em", context.globalOptOut ? copy.required : copy.optional));
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

    overlay.addEventListener("click", (event) => {
      const action = event.target.closest("[data-privacy-action]")?.dataset.privacyAction;
      if (!action) return;
      event.preventDefault();
      event.stopPropagation();
      if (action === "close") {
        overlay.remove();
        return;
      }
      if (action === "reject") categories.forEach((category) => { choices[category] = false; });
      if (action === "accept" && !context.globalOptOut) categories.forEach((category) => { choices[category] = true; });
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

    const content = document.createElement("div");
    content.className = "privacy-plugin-banner-content";
    content.append(text("strong", copy.bannerTitle));
    content.append(text("p", context.globalOptOut ? copy.gpc : copy.bannerMessage));
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
      const categories = [...new Set(context.gatedPlugins.map(categoryOf))];
      const choices = {};
      categories.forEach((category) => {
        choices[category] = action === "accept" && !context.globalOptOut;
      });
      const consent = saveConsent(action === "accept" && !context.globalOptOut ? "accepted" : "rejected", choices, context.country);
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
    const saved = readJson(storageKey);

    if (!gatedPlugins.length) {
      if (!globalOptOut) plugins.forEach(loadPlugin);
      return;
    }
    if (saved?.version === 3) {
      applyConsent(plugins, gatedPlugins, globalOptOut ? { choices: {} } : saved);
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
