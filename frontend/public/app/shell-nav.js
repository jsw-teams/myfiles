const path = location.pathname;

if (path.startsWith("/dashboard") || path.startsWith("/d/") || path.startsWith("/a/")) {
  bootDashboardShell().catch(() => {
    // 不阻断页面自身功能。
  });
}

async function bootDashboardShell() {
  const main = document.querySelector("main.pixel-shell");
  if (!main) return;

  // dashboard 首页已有原始 sidebar，先不重复注入。
  if (main.querySelector(":scope > .sidebar") || document.querySelector(".dashboard-side-nav")) {
    return;
  }

  injectStyles();

  const session = await loadSession();

  const frame = document.createElement("div");
  frame.className = "dashboard-frame";

  const aside = document.createElement("aside");
  aside.className = "dashboard-side-nav pixel-card";
  aside.innerHTML = renderSidebar(session);

  main.parentNode.insertBefore(frame, main);
  frame.appendChild(aside);
  frame.appendChild(main);

  document.body.classList.add("has-dashboard-shell");
  main.classList.add("dashboard-main");

  bindSidebar(aside);
}

async function loadSession() {
  try {
    const res = await fetch("/api/account/me", {
      credentials: "include",
      cache: "no-store"
    });

    if (!res.ok) {
      return null;
    }

    return await res.json();
  } catch {
    return null;
  }
}

function renderSidebar(session) {
  const user = session?.user || {};
  const perms = session?.myfilesPermissions || {};

  const canAdminFiles = Boolean(perms.allFilesRead || perms.allFilesWrite);
  const canAudit = Boolean(perms.auditRead);
  const canSettings = Boolean(perms.settingsRead || perms.settingsWrite || perms.storageSettings || perms.cdnSettings);
  const hasAdmin = canAdminFiles || canAudit || canSettings;

  const nav = [
    navItem("/dashboard", "控制台首页", "DASHBOARD", path === "/dashboard" || path === "/dashboard/"),
    navItem("/d/files", "我的文件", "MY FILES", path.startsWith("/dashboard/files") || path.startsWith("/d/files") || path.startsWith("/d/f/"))
  ];

  const adminNav = [];
  if (canAdminFiles) {
    adminNav.push(navItem("/a/files", "全部文件管理", "ADMIN FILES", path.startsWith("/dashboard/admin/files") || path.startsWith("/a/files")));
  }
  if (canAudit) {
    adminNav.push(navItem("/a/audit", "审计日志", "AUDIT", path.startsWith("/dashboard/admin/audit") || path.startsWith("/a/audit")));
  }
  if (canSettings) {
    adminNav.push(navItem("/a/settings", "站点设置", "SETTINGS", path.startsWith("/dashboard/admin/settings") || path.startsWith("/a/settings")));
  }

  return `
    <div class="dash-brand">
      <img src="/assets/mascot-files.png" alt="" />
      <div>
        <strong>myfiles</strong>
        <span>files.js.gripe</span>
      </div>
    </div>

    <div class="dash-user">
      <div class="dash-user-name">${escapeHtml(user.displayName || user.email || "未登录")}</div>
      <div class="dash-user-role">${escapeHtml(user.role || user.userType || "guest")}</div>
    </div>

    <nav class="dash-nav" aria-label="myfiles dashboard navigation">
      ${nav.join("")}

      ${
        hasAdmin
          ? `<div class="dash-nav-group">
              <div class="dash-nav-label">管理</div>
              ${adminNav.join("")}
            </div>`
          : ""
      }

      <div class="dash-nav-group">
        <div class="dash-nav-label">操作</div>
        <button type="button" class="dash-nav-item danger" id="dashboard-logout">
          <span>退出登录</span>
          <small>LOGOUT</small>
        </button>
      </div>
    </nav>
  `;
}

function navItem(href, label, kicker, active) {
  return `
    <a class="dash-nav-item ${active ? "active" : ""}" href="${href}" ${active ? 'aria-current="page"' : ""}>
      <span>${escapeHtml(label)}</span>
      <small>${escapeHtml(kicker)}</small>
    </a>
  `;
}

function bindSidebar(root) {
  root.querySelector("#dashboard-logout")?.addEventListener("click", async () => {
    try {
      await fetch("/api/auth/logout", {
        method: "POST",
        credentials: "include"
      });
    } finally {
      location.href = "/login";
    }
  });
}

function injectStyles() {
  if (document.querySelector("#dashboard-shell-styles")) return;

  const style = document.createElement("style");
  style.id = "dashboard-shell-styles";
  style.textContent = `
    .dashboard-frame {
      width: min(1480px, 100%);
      margin: 0 auto;
      padding: 28px;
      display: grid;
      grid-template-columns: 280px minmax(0, 1fr);
      gap: 22px;
      align-items: start;
    }

    .has-dashboard-shell main.pixel-shell.dashboard-main {
      max-width: none;
      margin: 0;
      padding: 0;
      min-width: 0;
    }

    .has-dashboard-shell main.pixel-shell.dashboard-main > .pixel-card {
      min-width: 0;
      overflow: auto;
    }

    .dashboard-side-nav {
      position: sticky;
      top: 24px;
      display: grid;
      gap: 18px;
      padding: 18px;
    }

    .dash-brand {
      display: flex;
      align-items: center;
      gap: 12px;
      padding-bottom: 14px;
      border-bottom: 2px solid rgba(242, 209, 107, 0.28);
    }

    .dash-brand img {
      width: 54px;
      height: 54px;
      image-rendering: pixelated;
    }

    .dash-brand strong {
      display: block;
      font-size: 18px;
      letter-spacing: 0.08em;
    }

    .dash-brand span,
    .dash-user-role,
    .dash-nav-item small,
    .dash-nav-label {
      color: var(--muted);
      font-size: 12px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }

    .dash-user {
      border: 2px solid rgba(242, 209, 107, 0.28);
      border-radius: 14px;
      padding: 12px;
      background: rgba(0, 0, 0, 0.18);
    }

    .dash-user-name {
      font-weight: 900;
      word-break: break-all;
    }

    .dash-nav {
      display: grid;
      gap: 10px;
    }

    .dash-nav-group {
      display: grid;
      gap: 10px;
      margin-top: 8px;
    }

    .dash-nav-label {
      margin-top: 4px;
      color: var(--accent);
    }

    .dash-nav-item {
      display: grid;
      gap: 3px;
      width: 100%;
      text-align: left;
      text-decoration: none;
      color: var(--text);
      border: 2px solid rgba(242, 209, 107, 0.35);
      border-radius: 12px;
      background: rgba(0, 0, 0, 0.18);
      padding: 12px;
      cursor: pointer;
      font: inherit;
    }

    .dash-nav-item:hover,
    .dash-nav-item.active {
      border-color: var(--accent);
      background: rgba(255, 206, 71, 0.12);
      box-shadow: 4px 4px 0 #000;
    }

    .dash-nav-item.danger {
      border-color: rgba(255, 111, 97, 0.55);
    }

    .dash-nav-item.danger:hover {
      border-color: var(--danger);
      background: rgba(255, 111, 97, 0.12);
    }

    @media (max-width: 900px) {
      .dashboard-frame {
        grid-template-columns: 1fr;
        padding: 16px;
      }

      .dashboard-side-nav {
        position: static;
      }

      .dash-nav {
        grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
      }

      .dash-nav-group {
        display: contents;
      }

      .dash-nav-label {
        display: none;
      }
    }
  `;

  document.head.appendChild(style);
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#039;"
  })[char]);
}
