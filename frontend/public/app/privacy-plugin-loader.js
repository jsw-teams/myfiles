(() => {
  const currentScript = document.currentScript;
  const configUrl = currentScript?.dataset.config || "/privacy-plugins.json";
  const stylesheetUrl = currentScript?.dataset.stylesheet || "/privacy-plugin-banner.css";
  const storageKey = "privacy_plugins_consent_v2";
  const regionKey = "privacy_plugins_region_v1";
  const consentRegionCodes = new Set([
    "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR",
    "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK",
    "SI", "ES", "SE", "IS", "LI", "NO", "GB", "UK", "CH", "BR", "CA", "CN",
    "HK", "MO", "TW", "ZA"
  ]);

  const fallbackCopy = {
    "zh-CN": {
      message: "我们使用隐私友好的分析插件了解页面访问情况。你可以选择是否启用这些非必要插件。",
      accept: "允许",
      decline: "拒绝"
    },
    "zh-TW": {
      message: "我們使用隱私友善的分析插件了解頁面訪問情況。你可以選擇是否啟用這些非必要插件。",
      accept: "允許",
      decline: "拒絕"
    },
    en: {
      message: "We use privacy-minded analytics plugins to understand page visits. You can choose whether to enable these optional plugins.",
      accept: "Allow",
      decline: "Decline"
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

  function loadStylesheet() {
    if ([...document.styleSheets].some((sheet) => sheet.href && sheet.href.endsWith(stylesheetUrl))) return;
    if (document.querySelector(`link[data-privacy-plugin-style][href="${stylesheetUrl}"]`)) return;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = stylesheetUrl;
    link.dataset.privacyPluginStyle = "banner";
    document.head.append(link);
  }

  function hasGlobalOptOut() {
    return navigator.globalPrivacyControl === true || navigator.doNotTrack === "1" || window.doNotTrack === "1";
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
    if (/Asia\/(Shanghai|Hong_Kong|Macau|Taipei)/.test(timezone)) return "CN";
    return "UNKNOWN";
  }

  function needsConsent(country, plugin, config) {
    const policy = plugin.policy || {};
    const mode = policy.consentMode || config.consent?.mode || policy.mode || "region-aware";
    if (mode === "always-prompt") return true;
    if (mode === "never-prompt") return false;
    const explicitCountries = new Set((policy.consentCountries || []).map(normalizeCountry));
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

  function showConsent(config, onChoice) {
    loadStylesheet();
    const lang = browserLang();
    const copy = config.consentBanner?.[lang] || config.consentBanner?.en || fallbackCopy[lang] || fallbackCopy.en;
    const banner = document.createElement("section");
    banner.className = "privacy-plugin-banner";
    banner.setAttribute("role", "dialog");
    banner.setAttribute("aria-live", "polite");
    banner.innerHTML = `
      <p>${copy.message}</p>
      <div>
        <button type="button" data-privacy-choice="decline">${copy.decline}</button>
        <button type="button" data-privacy-choice="accept">${copy.accept}</button>
      </div>
    `;

    document.body.append(banner);

    banner.addEventListener("click", (event) => {
      const button = event.target.closest("[data-privacy-choice]");
      if (!button) return;
      const granted = button.dataset.privacyChoice === "accept";
      writeJson(storageKey, { granted, time: Date.now() });
      banner.remove();
      onChoice(granted);
    });
  }

  async function boot() {
    if (hasGlobalOptOut()) return;
    const config = await fetch(configUrl, { cache: "no-store", credentials: "same-origin" }).then((response) => response.json());
    const plugins = (config.plugins || []).filter((plugin) => plugin && plugin.enabled !== false);
    if (!plugins.length) return;

    const country = await detectCountry();
    const gatedPlugins = plugins.filter((plugin) => needsConsent(country, plugin, config));
    const ungatedPlugins = plugins.filter((plugin) => !gatedPlugins.includes(plugin));
    ungatedPlugins.forEach(loadPlugin);
    if (!gatedPlugins.length) return;

    const saved = readJson(storageKey);
    if (saved?.granted === true) {
      gatedPlugins.forEach(loadPlugin);
      return;
    }
    if (saved?.granted === false) return;
    showConsent(config, (granted) => {
      if (granted) gatedPlugins.forEach(loadPlugin);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => boot().catch(() => {}), { once: true });
  } else {
    boot().catch(() => {});
  }
})();
