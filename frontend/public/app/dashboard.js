const app = document.querySelector('#dashboard-app');
const nav = document.querySelector('#nav');
const profile = document.querySelector('#profile');
const lang = (navigator.language || 'zh-CN').toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
const state = { me: null, files: [], adminFiles: [], fileVisible: 20, adminVisible: 30, selectedFiles: new Set(), selectedAdminFiles: new Set(), settings: {}, settingsGroup: 0 };

document.documentElement.lang = lang;

const L = {
  'zh-CN': {
    home: '概述', files: '我的文件', adminFiles: '全部文件管理', audit: '审计日志', settings: '站点设置', logout: '退出登录',
    dashboardDesc: '黑熊助手会把文件、取件码、审计和站点设置集中在这一个控制台路径里。',
    filesDesc: '查看上传批次取件码，或为单个文件建立临时取件码。',
    adminFilesDesc: '按文件名、所有者、文件 ID、SHA256 或 MIME 检索，并管理访问策略。',
    auditDesc: '按时间线查看谁在什么时间对什么对象做了什么，重点信息会被展开显示。',
    settingsDesc: '有写入权限的管理员可直接编辑并保存站点设置。',
    searchFiles: '搜索文件名 / 文件 ID / SHA256 / MIME', searchAdminFiles: '搜索文件名 / 所有者 / 文件 ID / SHA256 / MIME',
    open: '访问', pickup: '取件码', del: '删除', props: '属性', save: '保存设置', reload: '重新读取',
    emptyFiles: '没有符合条件的文件', emptyAudit: '暂无审计日志', loading: '正在加载...', noSettings: '暂无设置', saved: '设置已保存', saveFailed: '保存失败', createPickup: '建立文件取件码', tempPickup: '文件临时取件码', noPickup: '还没有有效取件码。', copy: '复制链接', revoke: '提前失效', uploadPickup: '上传取件码', confirmDelete: '确认软删除该文件？', confirmAdminDelete: '确认代管软删除？', owner: '所有者', public: '公开', private: '不公开', needConfirm: '需确认', noConfirm: '免确认',
    selectAll: '全选当前页', selected: '已选择', bulkDelete: '批量删除', bulkShare: '批量建立取件码', batchPickupReady: '批量取件码已建立', bulkPublic: '设为公开', bulkPrivate: '设为不公开', bulkConfirm: '设为需确认', bulkNoConfirm: '设为免确认', confirmBulkDelete: '确认删除选中的文件？', confirmBulkAdminDelete: '确认代管删除选中的文件？', pageSize: '每页显示', loadMore: '加载更多', showing: '当前显示', settingsPrev: '上一组', settingsNext: '下一组', settingsGroup: '设置分组',
    auditIpSearch: '按 IP 地址搜索', auditLimit: '显示数量',
    yes: '是', no: '否',
    regionGlobal: '不限制地区', regionAllowOnly: '仅允许这些地区', regionDenyOnly: '排除这些地区', regionCodes: '地区代码', regionHint: '使用 ISO 国家/地区代码，用逗号分隔；仅会保存当前启用的一种方案。', regionQuick: '常用地区', regionClear: '清空地区', hotlinkAllow: '允许热链', hotlinkDeny: '禁止热链'
  },
  en: {
    home: 'Overview', files: 'My files', adminFiles: 'All files', audit: 'Audit log', settings: 'Settings', logout: 'Sign out',
    dashboardDesc: 'The bear assistant keeps files, pickup codes, audit events, and settings in one dashboard path.',
    filesDesc: 'View upload pickup codes or create temporary pickup codes for individual files.',
    adminFilesDesc: 'Search by name, owner, file ID, SHA256, or MIME, and manage access policy.',
    auditDesc: 'Review who did what, when, and to which target. Important details are expanded.',
    settingsDesc: 'Administrators with write permission can edit and save site settings here.',
    searchFiles: 'Search filename / file ID / SHA256 / MIME', searchAdminFiles: 'Search filename / owner / file ID / SHA256 / MIME',
    open: 'Open', pickup: 'Pickup code', del: 'Delete', props: 'Properties', save: 'Save settings', reload: 'Reload',
    emptyFiles: 'No matching files', emptyAudit: 'No audit events yet', loading: 'Loading...', noSettings: 'No settings yet', saved: 'Settings saved', saveFailed: 'Save failed', createPickup: 'Create file pickup code', tempPickup: 'Temporary file pickup code', noPickup: 'No active pickup code yet.', copy: 'Copy link', revoke: 'Revoke early', uploadPickup: 'Upload pickup code', confirmDelete: 'Soft-delete this file?', confirmAdminDelete: 'Soft-delete this file as admin?', owner: 'Owner', public: 'Public', private: 'Private', needConfirm: 'Confirm', noConfirm: 'No confirm',
    selectAll: 'Select page', selected: 'selected', bulkDelete: 'Delete selected', bulkShare: 'Create pickup code', batchPickupReady: 'Batch pickup code created', bulkPublic: 'Make public', bulkPrivate: 'Make private', bulkConfirm: 'Require confirm', bulkNoConfirm: 'No confirmation', confirmBulkDelete: 'Delete selected files?', confirmBulkAdminDelete: 'Admin-delete selected files?', pageSize: 'Page size', loadMore: 'Load more', showing: 'Showing', settingsPrev: 'Previous', settingsNext: 'Next', settingsGroup: 'Settings group',
    auditIpSearch: 'Search by IP address', auditLimit: 'Rows',
    yes: 'Yes', no: 'No',
    regionGlobal: 'No region limit', regionAllowOnly: 'Allow only these regions', regionDenyOnly: 'Block these regions', regionCodes: 'Region codes', regionHint: 'Use ISO country/region codes separated by commas. Only the active mode is saved.', regionQuick: 'Common regions', regionClear: 'Clear regions', hotlinkAllow: 'Allow hotlinking', hotlinkDeny: 'Block hotlinking'
  }
}[lang];

const views = {
  home: { label: L.home, kicker: L.home, icon: 'home' },
  files: { label: L.files, kicker: L.files, icon: 'files' },
  'admin-files': { label: L.adminFiles, kicker: L.adminFiles, icon: 'folderCog', need: 'allFilesRead' },
  audit: { label: L.audit, kicker: L.audit, icon: 'clipboardList', need: 'auditRead' },
  settings: { label: L.settings, kicker: L.settings, icon: 'settings', need: 'settingsWrite' }
};

const iconPaths = {
  home: '<path d="m3 9 9-7 9 7"></path><path d="M9 22V12h6v10"></path><path d="M21 9v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9"></path>',
  files: '<path d="M20 7h-3a2 2 0 0 1-2-2V2"></path><path d="M9 18a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h7l4 4v10a2 2 0 0 1-2 2Z"></path><path d="M3 7.6v12.8A1.6 1.6 0 0 0 4.6 22h9.8"></path>',
  folderCog: '<path d="M10.5 20H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h4.5L11 6h9a2 2 0 0 1 2 2v3.5"></path><circle cx="17" cy="17" r="3"></circle><path d="M17 13.5V12"></path><path d="M17 22v-1.5"></path><path d="m20 14.5-1.3.8"></path><path d="m15.3 18.7-1.3.8"></path><path d="m20 19.5-1.3-.8"></path><path d="m15.3 15.3-1.3-.8"></path>',
  clipboardList: '<rect width="8" height="4" x="8" y="2" rx="1"></rect><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"></path><path d="M12 11h4"></path><path d="M12 16h4"></path><path d="M8 11h.01"></path><path d="M8 16h.01"></path>',
  settings: '<path d="M9.7 3.4a1.6 1.6 0 0 1 3 0l.2.8a1.6 1.6 0 0 0 2.3 1l.7-.4a1.6 1.6 0 0 1 2.2 2.2l-.4.7a1.6 1.6 0 0 0 1 2.3l.8.2a1.6 1.6 0 0 1 0 3l-.8.2a1.6 1.6 0 0 0-1 2.3l.4.7a1.6 1.6 0 0 1-2.2 2.2l-.7-.4a1.6 1.6 0 0 0-2.3 1l-.2.8a1.6 1.6 0 0 1-3 0l-.2-.8a1.6 1.6 0 0 0-2.3-1l-.7.4a1.6 1.6 0 0 1-2.2-2.2l.4-.7a1.6 1.6 0 0 0-1-2.3l-.8-.2a1.6 1.6 0 0 1 0-3l.8-.2a1.6 1.6 0 0 0 1-2.3l-.4-.7a1.6 1.6 0 0 1 2.2-2.2l.7.4a1.6 1.6 0 0 0 2.3-1z"></path><circle cx="12" cy="12" r="3"></circle>',
  logOut: '<path d="m16 17 5-5-5-5"></path><path d="M21 12H9"></path><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path>'
};

const settingFields = [
  ['site', 'site.brandName', '站点名称', 'Site name', 'text'],
  ['upload', 'upload.maxMB', '上传上限 MB', 'Upload limit MB', 'number'], ['upload', 'upload.allowAnonymous', '允许匿名上传', 'Allow anonymous upload', 'bool'], ['upload', 'upload.allowedMimeTypes', '允许 MIME 类型', 'Allowed MIME types', 'textarea'],
  ['file', 'file.defaultPublic', '默认公开', 'Default public', 'bool'], ['file', 'file.defaultRequireConfirm', '访问前确认', 'Require confirmation', 'bool'], ['file', 'file.defaultRegionPolicy', '地区策略', 'Region policy', 'region'], ['file', 'file.defaultHotlinkPolicy', '热链策略', 'Hotlink policy', 'hotlink'],
  ['security', 'security.sessionTtlHours', '会话有效小时', 'Session TTL hours', 'number'], ['audit', 'audit.retentionDays', '审计保留天数', 'Audit retention days', 'number']
];

const settingGroups = [
  ['site', '站点信息', 'Site'],
  ['upload', '上传规则', 'Upload'],
  ['file', '文件默认策略', 'File defaults'],
  ['security', '安全', 'Security'],
  ['audit', '审计', 'Audit']
];

boot().catch(() => { location.href = '/login'; });
async function boot() { state.me = await api('/api/account/me'); const user = state.me.user || {}; profile.textContent = `${user.displayName || user.email || user.id} · ${user.role || user.userType}`; renderNav(); window.addEventListener('hashchange', renderRoute); renderRoute(); }
function allowed(view) { const need = views[view]?.need; return !need || Boolean(state.me?.myfilesPermissions?.[need]); }
function currentView() { const name = location.hash.replace('#', '') || 'home'; return views[name] && allowed(name) ? name : 'home'; }
function renderNav() { nav.innerHTML = Object.entries(views).filter(([key]) => allowed(key)).map(([key, item]) => `<a class="dash-nav-item" href="/dashboard#${key}" data-view-link="${key}"><span>${iconSvg(item.icon)}${escapeHtml(item.label)}</span></a>`).join('') + `<button id="logout" class="dash-nav-item danger" type="button"><span>${iconSvg('logOut')}${escapeHtml(L.logout)}</span></button>`; document.querySelector('#logout').onclick = async () => { await api('/api/auth/logout', { method: 'POST' }); location.href = '/'; }; }
async function renderRoute() { const view = currentView(); document.querySelectorAll('[data-view-link]').forEach((link) => link.classList.toggle('active', link.dataset.viewLink === view)); if (view === 'home') return renderHome(); if (view === 'files') return renderFiles(); if (view === 'admin-files') return renderAdminFiles(); if (view === 'audit') return renderAudit(); if (view === 'settings') return renderSettings(); }
function shell(title, kicker, desc, body) { app.innerHTML = `<section class="pixel-card dashboard-panel grid"><header class="page-head"><div class="bear-helper"><img src="/assets/mascot-files.png" alt="" /></div><div><div class="pixel-kicker">${escapeHtml(kicker)}</div><h1>${escapeHtml(title)}</h1><p class="muted">${escapeHtml(desc)}</p></div></header>${body}</section>`; }
function renderHome() { const cards = Object.entries(views).filter(([key]) => key !== 'home' && allowed(key)).map(([key, item]) => `<a class="pixel-card dashboard-card" href="/dashboard#${key}"><span class="icon" aria-hidden="true">${iconSvg(item.icon)}</span><h3>${escapeHtml(item.label)}</h3></a>`).join(''); shell(L.home, L.home, L.dashboardDesc, `<div class="grid two">${cards}</div>`); }
async function renderFiles() { state.selectedFiles.clear(); shell(L.files, L.files, L.filesDesc, `<div class="toolbar"><label class="sr-only" for="files-q">${escapeHtml(L.searchFiles)}</label><input id="files-q" class="input" autocomplete="off" placeholder="${escapeAttr(L.searchFiles)}" /><label class="page-size"><span>${escapeHtml(L.pageSize)}</span><select id="files-size" class="input"><option>10</option><option selected>20</option><option>50</option></select></label></div><div class="bulk-bar"><label><input type="checkbox" data-select-page="files" /> ${escapeHtml(L.selectAll)}</label><span id="files-selected">0 ${escapeHtml(L.selected)}</span><button class="pixel-button compact" data-bulk-share="files" type="button">${escapeHtml(L.bulkShare)}</button><button class="pixel-button danger compact" data-bulk-delete="files" type="button">${escapeHtml(L.bulkDelete)}</button></div><div id="files-list" class="data-list list-frame" aria-live="polite">${skeletonCards(6)}</div><div id="files-pager" class="pager"></div>`); const q = document.querySelector('#files-q'); const size = document.querySelector('#files-size'); q.addEventListener('input', debounce(() => loadFiles(q.value), 180)); size.addEventListener('change', () => { state.fileVisible = Number(size.value || 20); renderFileList(); }); await loadFiles(''); }
async function loadFiles(query) { const list = document.querySelector('#files-list'); list.innerHTML = skeletonCards(6); const json = await api('/api/files?q=' + encodeURIComponent(query || '') + '&limit=200'); state.files = json.files || []; state.selectedFiles.clear(); renderFileList(); }
function renderFileList() { const visible = state.files.slice(0, state.fileVisible); document.querySelector('#files-list').innerHTML = visible.map((f) => fileCard(f, 'files')).join('') || empty(L.emptyFiles); document.querySelector('#files-pager').innerHTML = pagerHTML(visible.length, state.files.length, 'files'); updateBulkUI('files'); }
function fileCard(f, scope = 'files') { const selected = scope === 'admin' ? state.selectedAdminFiles.has(f.id) : state.selectedFiles.has(f.id); const pickup = f.uploadPickupCode ? `<div class="pickup-mini"><span>${escapeHtml(L.uploadPickup)}</span><strong>${escapeHtml(f.uploadPickupCode)}</strong></div>` : ''; return `<article class="data-card"><label class="select-cell"><input type="checkbox" data-select-${scope}="${escapeAttr(f.id)}" ${selected ? 'checked' : ''} /><span class="sr-only">${escapeHtml(f.originalName)}</span></label><div><h3>${escapeHtml(f.originalName)}</h3><p class="muted">${escapeHtml(f.mime)} · ${formatSize(f.size)} · ${escapeHtml(f.createdAt)}</p><small>${escapeHtml(f.id)}<br>${escapeHtml(f.sha256)}</small>${pickup}</div><div class="card-actions"><a class="pixel-button secondary compact" href="${escapeAttr(f.publicUrl || publicFilePath(f))}" target="_blank">${escapeHtml(L.open)}</a><a class="pixel-button compact" href="/dashboard#file-${escapeAttr(f.id)}" data-file-detail="${escapeAttr(f.id)}">${escapeHtml(L.pickup)}</a><button class="pixel-button danger compact" data-delete-file="${escapeAttr(f.id)}">${escapeHtml(L.del)}</button></div></article>`; }
async function renderAdminFiles() { state.selectedAdminFiles.clear(); shell(L.adminFiles, L.adminFiles, L.adminFilesDesc, `<div class="toolbar"><label class="sr-only" for="admin-files-q">${escapeHtml(L.searchAdminFiles)}</label><input id="admin-files-q" class="input" autocomplete="off" placeholder="${escapeAttr(L.searchAdminFiles)}" /><label class="page-size"><span>${escapeHtml(L.pageSize)}</span><select id="admin-files-size" class="input"><option>15</option><option selected>30</option><option>75</option></select></label></div><div class="bulk-bar"><label><input type="checkbox" data-select-page="admin" /> ${escapeHtml(L.selectAll)}</label><span id="admin-selected">0 ${escapeHtml(L.selected)}</span><button class="pixel-button secondary compact" data-bulk-policy="public" type="button">${escapeHtml(L.bulkPublic)}</button><button class="pixel-button secondary compact" data-bulk-policy="private" type="button">${escapeHtml(L.bulkPrivate)}</button><button class="pixel-button secondary compact" data-bulk-policy="confirm" type="button">${escapeHtml(L.bulkConfirm)}</button><button class="pixel-button secondary compact" data-bulk-policy="no-confirm" type="button">${escapeHtml(L.bulkNoConfirm)}</button><button class="pixel-button danger compact" data-bulk-delete="admin" type="button">${escapeHtml(L.bulkDelete)}</button></div><div id="admin-files-list" class="data-list list-frame" aria-live="polite">${skeletonCards(8)}</div><div id="admin-files-pager" class="pager"></div>`); const q = document.querySelector('#admin-files-q'); const size = document.querySelector('#admin-files-size'); q.addEventListener('input', debounce(() => loadAdminFiles(q.value), 180)); size.addEventListener('change', () => { state.adminVisible = Number(size.value || 30); renderAdminFileList(); }); await loadAdminFiles(''); }
async function loadAdminFiles(query) { const list = document.querySelector('#admin-files-list'); list.innerHTML = skeletonCards(8); const json = await api('/api/admin/files?q=' + encodeURIComponent(query || '') + '&owner=' + encodeURIComponent(query || '') + '&limit=300'); state.adminFiles = json.files || []; state.selectedAdminFiles.clear(); renderAdminFileList(); }
function renderAdminFileList() { const visible = state.adminFiles.slice(0, state.adminVisible); document.querySelector('#admin-files-list').innerHTML = visible.map(adminFileCard).join('') || empty(L.emptyFiles); document.querySelector('#admin-files-pager').innerHTML = pagerHTML(visible.length, state.adminFiles.length, 'admin'); updateBulkUI('admin'); }
function adminFileCard(f) { const selected = state.selectedAdminFiles.has(f.id); return `<article class="data-card"><label class="select-cell"><input type="checkbox" data-select-admin="${escapeAttr(f.id)}" ${selected ? 'checked' : ''} /><span class="sr-only">${escapeHtml(f.originalName)}</span></label><div><h3>${escapeHtml(f.originalName)}</h3><p class="muted">${escapeHtml(L.owner)}: ${escapeHtml(f.ownerUserId || 'anonymous')} · ${escapeHtml(f.status)}</p><small>${escapeHtml(f.id)}<br>${escapeHtml(f.sha256)}</small><div class="policy-row"><span>${f.isPublic ? L.public : L.private}</span><span>${f.requireConfirm ? L.needConfirm : L.noConfirm}</span><span>${escapeHtml(f.regionPolicy)}/${escapeHtml(f.hotlinkPolicy)}</span></div></div><div class="card-actions"><a class="pixel-button secondary compact" href="/admin/open/${escapeAttr(f.id)}" target="_blank">${escapeHtml(L.open)}</a><button class="pixel-button danger compact" data-admin-delete="${escapeAttr(f.id)}">${escapeHtml(L.del)}</button></div></article>`; }
async function renderAudit() {
  shell(L.audit, L.audit, L.auditDesc, `<div class="toolbar"><label class="sr-only" for="audit-ip">${escapeHtml(L.auditIpSearch)}</label><input id="audit-ip" class="input" inputmode="numeric" autocomplete="off" placeholder="${escapeAttr(L.auditIpSearch)}" /><label class="page-size"><span>${escapeHtml(L.auditLimit)}</span><select id="audit-limit" class="input"><option>25</option><option selected>50</option><option>100</option><option>200</option></select></label></div><div id="audit-list" class="audit-list list-frame" aria-live="polite">${skeletonAudit(6)}</div>`);
  const ip = document.querySelector('#audit-ip');
  const limitSelect = document.querySelector('#audit-limit');
  ip.addEventListener('input', debounce(loadAudit, 220));
  limitSelect.addEventListener('change', loadAudit);
  await loadAudit();
}
async function loadAudit() {
  const list = document.querySelector('#audit-list');
  const ip = document.querySelector('#audit-ip')?.value || '';
  const limitValue = document.querySelector('#audit-limit')?.value || '50';
  list.innerHTML = skeletonAudit(6);
  const json = await api('/api/admin/audit?ip=' + encodeURIComponent(ip) + '&limit=' + encodeURIComponent(limitValue));
  list.innerHTML = (json.logs || []).map(auditCard).join('') || empty(L.emptyAudit);
}
function auditCard(l) { const detail = typeof l.detail === 'object' ? l.detail : {}; return `<article class="audit-card"><div class="audit-time">${escapeHtml(formatTime(l.createdAt))}</div><div><h3>${escapeHtml(actionLabel(l.action))}</h3><p class="muted">${escapeHtml(l.action)} · ${escapeHtml(l.targetType)} ${escapeHtml(l.targetId || '')}</p>${detailSummary(detail)}</div><div class="audit-actor"><strong>${escapeHtml(l.actorRole || 'system')}</strong><span>${escapeHtml(l.actorAccountUserId || 'system')}</span><small>${escapeHtml(l.ip || '')}</small></div></article>`; }
function actionLabel(action) { const zh = { 'upload.create': '创建上传批次', 'file.delete': '删除文件', 'share.create': '建立文件取件码', 'share.revoke': '提前失效取件码', 'pickup.read': '读取取件码', 'pickup.download': '取件下载', 'admin.file.patch': '修改文件策略', 'admin.file.delete': '代管删除文件', 'settings.patch': '修改站点设置', 'auth.login': '登录', 'auth.logout': '退出登录' }; const en = { 'upload.create': 'Upload batch created', 'file.delete': 'File deleted', 'share.create': 'Pickup code created', 'share.revoke': 'Pickup code revoked', 'pickup.read': 'Pickup code read', 'pickup.download': 'Pickup download', 'admin.file.patch': 'File policy changed', 'admin.file.delete': 'Admin file delete', 'settings.patch': 'Settings changed', 'auth.login': 'Signed in', 'auth.logout': 'Signed out' }; return (lang === 'zh-CN' ? zh : en)[action] || action; }
function detailSummary(detail) { const entries = Object.entries(detail || {}).slice(0, 6); return entries.length ? `<dl class="detail-list">${entries.map(([k, v]) => `<div><dt>${escapeHtml(detailKeyLabel(k))}</dt><dd>${escapeHtml(detailValue(v))}</dd></div>`).join('')}</dl>` : ''; }
function detailKeyLabel(key) {
  const zh = { fileId: '文件 ID', file_id: '文件 ID', fileName: '文件名', originalName: '原始文件名', ownerUserId: '所有者用户 ID', owner_user_id: '所有者用户 ID', pickupCode: '取件码', pickup_code: '取件码', size: '大小', mime: 'MIME 类型', status: '状态', expiresAt: '过期时间', expires_at: '过期时间', changes: '变更内容', path: '路径', reason: '原因' };
  const en = { fileId: 'File ID', file_id: 'File ID', fileName: 'Filename', originalName: 'Original name', ownerUserId: 'Owner user ID', owner_user_id: 'Owner user ID', pickupCode: 'Pickup code', pickup_code: 'Pickup code', size: 'Size', mime: 'MIME type', status: 'Status', expiresAt: 'Expires at', expires_at: 'Expires at', changes: 'Changes', path: 'Path', reason: 'Reason' };
  return (lang === 'zh-CN' ? zh : en)[key] || key;
}
function detailValue(value) {
  if (value == null) return '';
  if (typeof value === 'boolean') return value ? (lang === 'zh-CN' ? '是' : 'Yes') : (lang === 'zh-CN' ? '否' : 'No');
  if (typeof value === 'number') return String(value);
  if (Array.isArray(value)) return value.map(detailValue).join(', ');
  if (typeof value === 'object') return Object.entries(value).map(([k, v]) => `${detailKeyLabel(k)}: ${detailValue(v)}`).join('; ');
  return String(value);
}
async function renderSettings() { state.settingsGroup = 0; shell(L.settings, L.settings, L.settingsDesc, `<form id="settings-lite-form" class="settings-lite grid"><div class="toolbar"><button id="settings-load" class="pixel-button secondary" type="button">${escapeHtml(L.reload)}</button><button class="pixel-button" type="submit">${escapeHtml(L.save)}</button></div><div id="settings-tabs" class="tabs" aria-label="${escapeAttr(L.settingsGroup)}"></div><div id="settings-box" class="form-grid settings-frame">${skeletonFields(4)}</div><div id="settings-pager" class="pager"></div><div id="settings-alert" role="alert"></div></form>`); document.querySelector('#settings-load').onclick = loadSettingsLite; document.querySelector('#settings-lite-form').addEventListener('submit', saveSettingsLite); await loadSettingsLite(); }
async function loadSettingsLite() { const box = document.querySelector('#settings-box'); box.innerHTML = skeletonFields(4); const json = await api('/api/admin/settings'); state.settings = json.settings || {}; renderSettingsGroup(); }
function renderSettingsGroup() {
  const group = settingGroups[state.settingsGroup] || settingGroups[0];
  const fields = settingFields.filter(([groupKey]) => groupKey === group[0]);
  document.querySelector('#settings-tabs').innerHTML = settingGroups.map((item, index) => `<button class="tab ${index === state.settingsGroup ? 'active' : ''}" type="button" data-settings-group="${index}">${escapeHtml(lang === 'zh-CN' ? item[1] : item[2])}</button>`).join('');
  document.querySelector('#settings-box').innerHTML = fields.map(([, key, zh, en, type]) => fieldHTML(key, lang === 'zh-CN' ? zh : en, state.settings[key]?.value ?? state.settings[key], type, false)).join('') || empty(L.noSettings);
  document.querySelector('#settings-pager').innerHTML = `<span>${escapeHtml(lang === 'zh-CN' ? group[1] : group[2])} ${state.settingsGroup + 1}/${settingGroups.length}</span><button class="pixel-button secondary compact" type="button" data-settings-prev ${state.settingsGroup === 0 ? 'disabled' : ''}>${escapeHtml(L.settingsPrev)}</button><button class="pixel-button secondary compact" type="button" data-settings-next ${state.settingsGroup >= settingGroups.length - 1 ? 'disabled' : ''}>${escapeHtml(L.settingsNext)}</button>`;
}
function fieldHTML(key, label, value, type, disabled) {
  const attrs = `name="${escapeAttr(key)}" class="input" ${disabled ? 'disabled' : ''}`;
  if (type === 'bool') return choiceField(key, label, value === true, [[true, L.yes], [false, L.no]]);
  if (type === 'region') return regionPolicyField(key, label, value || 'global');
  if (type === 'hotlink') return choiceField(key, label, value || 'allow', [['allow', L.hotlinkAllow], ['deny', L.hotlinkDeny]]);
  if (type === 'textarea') return `<label class="field-block"><span>${escapeHtml(label)}</span><textarea ${attrs} rows="3">${escapeHtml(Array.isArray(value) ? value.join('\n') : value ?? '')}</textarea></label>`;
  return `<label class="field-block"><span>${escapeHtml(label)}</span><input ${attrs} type="${type}" value="${escapeAttr(value ?? '')}" /></label>`;
}
function choiceField(key, label, value, options) {
  return `<fieldset class="choice-field"><legend>${escapeHtml(label)}</legend><div class="segmented">${options.map(([optionValue, optionLabel]) => {
    const id = `${key}-${String(optionValue)}`.replace(/[^a-z0-9_-]/gi, '-');
    return `<label class="segment"><input id="${escapeAttr(id)}" type="radio" name="${escapeAttr(key)}" value="${escapeAttr(optionValue)}" ${String(optionValue) === String(value) ? 'checked' : ''} /><span>${escapeHtml(optionLabel)}</span></label>`;
  }).join('')}</div></fieldset>`;
}
function regionPolicyField(key, label, value) {
  const parsed = parseRegionPolicy(value);
  const options = [['global', L.regionGlobal], ['allow', L.regionAllowOnly], ['deny', L.regionDenyOnly]];
  return `<fieldset class="choice-field region-policy" data-region-policy>
    <legend>${escapeHtml(label)}</legend>
    <div class="segmented">${options.map(([optionValue, optionLabel]) => {
      const id = `${key}-${optionValue}`.replace(/[^a-z0-9_-]/gi, '-');
      return `<label class="segment"><input id="${escapeAttr(id)}" type="radio" name="${escapeAttr(key)}.mode" value="${escapeAttr(optionValue)}" data-region-mode ${optionValue === parsed.mode ? 'checked' : ''} /><span>${escapeHtml(optionLabel)}</span></label>`;
    }).join('')}</div>
    <div class="region-editor" ${parsed.mode === 'global' ? 'hidden' : ''}>
      <label class="field-block compact"><span>${escapeHtml(L.regionCodes)}</span><input class="input" data-region-codes inputmode="latin" autocomplete="off" value="${escapeAttr(parsed.codes.join(', '))}" placeholder="CN, HK, US" /></label>
      <div class="region-presets" aria-label="${escapeAttr(L.regionQuick)}">${['CN','HK','MO','TW','US','JP','SG','DE','FR','GB'].map((code) => `<button class="chip-button" type="button" data-region-chip="${code}">${code}</button>`).join('')}<button class="chip-button danger" type="button" data-region-clear>${escapeHtml(L.regionClear)}</button></div>
      <p class="muted">${escapeHtml(L.regionHint)}</p>
    </div>
    <input type="hidden" name="${escapeAttr(key)}" data-region-value value="${escapeAttr(formatRegionPolicy(parsed.mode, parsed.codes))}" />
  </fieldset>`;
}
async function saveSettingsLite(event) { event.preventDefault(); if (!state.me?.myfilesPermissions?.settingsWrite) return; const form = event.currentTarget; const body = {}; new FormData(form).forEach((raw, key) => { if (!key) return; body[key] = normalizeSetting(key, raw); }); const alert = document.querySelector('#settings-alert'); try { await api('/api/admin/settings', { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); alert.className = 'pixel-alert ok'; alert.textContent = L.saved; } catch (error) { alert.className = 'pixel-alert'; alert.textContent = `${L.saveFailed}: ${error.message}`; } }
function normalizeSetting(key, raw) { const value = String(raw ?? '').trim(); if (['upload.allowAnonymous', 'file.defaultPublic', 'file.defaultRequireConfirm'].includes(key)) return value === 'true'; if (['upload.maxMB', 'security.sessionTtlHours', 'audit.retentionDays'].includes(key)) return Number(value || 0); if (key === 'upload.allowedMimeTypes') return value.split(/\n|,/).map((item) => item.trim()).filter(Boolean); return value; }
app.addEventListener('click', async (event) => {
  const detail = event.target.closest('[data-file-detail]');
  if (detail) { event.preventDefault(); return renderFileDetail(detail.dataset.fileDetail); }
  const del = event.target.closest('[data-delete-file]');
  if (del && confirm(L.confirmDelete)) { await api('/api/files/' + encodeURIComponent(del.dataset.deleteFile), { method: 'DELETE' }); return renderFiles(); }
  const adminDel = event.target.closest('[data-admin-delete]');
  if (adminDel && confirm(L.confirmAdminDelete)) { await api('/api/admin/files/' + encodeURIComponent(adminDel.dataset.adminDelete), { method: 'DELETE' }); return renderAdminFiles(); }
  const more = event.target.closest('[data-load-more]');
  if (more?.dataset.loadMore === 'files') { state.fileVisible += state.fileVisible; return renderFileList(); }
  if (more?.dataset.loadMore === 'admin') { state.adminVisible += state.adminVisible; return renderAdminFileList(); }
  const bulkDelete = event.target.closest('[data-bulk-delete]');
  if (bulkDelete) return runBulkDelete(bulkDelete.dataset.bulkDelete);
  const bulkShare = event.target.closest('[data-bulk-share]');
  if (bulkShare) return runBulkShare();
  const bulkPolicy = event.target.closest('[data-bulk-policy]');
  if (bulkPolicy) return runBulkAdminPolicy(bulkPolicy.dataset.bulkPolicy);
  const group = event.target.closest('[data-settings-group]');
  if (group) { state.settingsGroup = Number(group.dataset.settingsGroup || 0); return renderSettingsGroup(); }
  if (event.target.closest('[data-settings-prev]')) { state.settingsGroup = Math.max(0, state.settingsGroup - 1); return renderSettingsGroup(); }
  if (event.target.closest('[data-settings-next]')) { state.settingsGroup = Math.min(settingGroups.length - 1, state.settingsGroup + 1); return renderSettingsGroup(); }
  const chip = event.target.closest('[data-region-chip]');
  if (chip) { toggleRegionChip(chip.closest('[data-region-policy]'), chip.dataset.regionChip); return; }
  if (event.target.closest('[data-region-clear]')) { const root = event.target.closest('[data-region-policy]'); const input = root?.querySelector('[data-region-codes]'); if (input) input.value = ''; updateRegionPolicy(root); }
});
app.addEventListener('change', (event) => {
  const file = event.target.closest('[data-select-files]');
  if (file) { toggleSet(state.selectedFiles, file.dataset.selectFiles, file.checked); return updateBulkUI('files'); }
  const admin = event.target.closest('[data-select-admin]');
  if (admin) { toggleSet(state.selectedAdminFiles, admin.dataset.selectAdmin, admin.checked); return updateBulkUI('admin'); }
  const page = event.target.closest('[data-select-page]');
  if (page) return selectVisible(page.dataset.selectPage, page.checked);
  if (event.target.closest('[data-region-mode]')) return updateRegionPolicy(event.target.closest('[data-region-policy]'));
});
app.addEventListener('input', (event) => {
  if (event.target.closest('[data-region-codes]')) updateRegionPolicy(event.target.closest('[data-region-policy]'));
});
async function renderFileDetail(id) { const json = await api('/api/files/' + encodeURIComponent(id)); const f = json.file; const shares = json.shares || []; shell(L.pickup, L.pickup, f.originalName, `<div class="data-list">${fileCard(f)}<article class="data-card"><div><h3>${escapeHtml(L.tempPickup)}</h3><p class="muted">${escapeHtml(L.filesDesc)}</p>${shares.map(renderShare).join('') || `<p class="muted">${escapeHtml(L.noPickup)}</p>`}</div><div class="card-actions"><button class="pixel-button compact" data-create-share="${escapeAttr(f.id)}">${escapeHtml(L.createPickup)}</button></div></article></div>`); document.querySelector('[data-create-share]')?.addEventListener('click', async (event) => { event.currentTarget.disabled = true; await api('/api/files/' + encodeURIComponent(f.id) + '/share', { method: 'POST' }); await renderFileDetail(f.id); }); }
function renderShare(share) { return `<div class="share-chip"><div><span>${escapeHtml(L.pickup)}</span><strong>${escapeHtml(share.pickupCode)}</strong><small>${escapeHtml(formatTime(share.pickupExpiresAt))}</small></div><div class="card-actions"><button class="pixel-button secondary compact" data-copy="${escapeAttr(location.origin + '/?code=' + encodeURIComponent(share.pickupCode))}">${escapeHtml(L.copy)}</button><button class="pixel-button danger compact" data-revoke="${escapeAttr(share.pickupCode)}">${escapeHtml(L.revoke)}</button></div></div>`; }
app.addEventListener('click', async (event) => { const copy = event.target.closest('[data-copy]'); if (copy) { await navigator.clipboard?.writeText(copy.dataset.copy || ''); copy.textContent = lang === 'zh-CN' ? '已复制' : 'Copied'; } const revoke = event.target.closest('[data-revoke]'); if (revoke) { await api('/api/shares/' + encodeURIComponent(revoke.dataset.revoke), { method: 'DELETE' }); revoke.closest('.share-chip')?.remove(); } });
function toggleSet(set, id, checked) { if (!id) return; checked ? set.add(id) : set.delete(id); }
function selectVisible(scope, checked) {
  const items = scope === 'admin' ? state.adminFiles.slice(0, state.adminVisible) : state.files.slice(0, state.fileVisible);
  const set = scope === 'admin' ? state.selectedAdminFiles : state.selectedFiles;
  items.forEach((f) => toggleSet(set, f.id, checked));
  scope === 'admin' ? renderAdminFileList() : renderFileList();
}
function selectedIds(scope) { return Array.from(scope === 'admin' ? state.selectedAdminFiles : state.selectedFiles); }
function updateBulkUI(scope) {
  const ids = selectedIds(scope);
  const label = document.querySelector(scope === 'admin' ? '#admin-selected' : '#files-selected');
  if (label) label.textContent = `${ids.length} ${L.selected}`;
  document.querySelectorAll(scope === 'admin' ? '[data-bulk-delete="admin"], [data-bulk-policy]' : '[data-bulk-delete="files"], [data-bulk-share="files"]').forEach((button) => { button.disabled = ids.length === 0; });
}
async function runBulkDelete(scope) {
  const ids = selectedIds(scope);
  if (!ids.length) return;
  if (!confirm(scope === 'admin' ? L.confirmBulkAdminDelete : L.confirmBulkDelete)) return;
  const path = scope === 'admin' ? '/api/admin/files/batch' : '/api/files/batch';
  await api(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'delete', fileIds: ids }) });
  scope === 'admin' ? await loadAdminFiles(document.querySelector('#admin-files-q')?.value || '') : await loadFiles(document.querySelector('#files-q')?.value || '');
}
async function runBulkShare() {
  const ids = selectedIds('files');
  if (!ids.length) return;
  const json = await api('/api/files/batch', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'share', fileIds: ids }) });
  const url = location.origin + (json.url || ('/?code=' + encodeURIComponent(json.share?.pickupCode || '')));
  await navigator.clipboard?.writeText(url);
  alert(`${L.batchPickupReady}: ${json.share?.pickupCode || ''}`);
}
async function runBulkAdminPolicy(action) {
  const ids = selectedIds('admin');
  if (!ids.length) return;
  await api('/api/admin/files/batch', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action, fileIds: ids }) });
  await loadAdminFiles(document.querySelector('#admin-files-q')?.value || '');
}
async function api(path, opts = {}) { const res = await fetch(path, { credentials: 'include', ...opts }); const json = await res.json().catch(() => ({})); if (!res.ok || json.ok === false) throw new Error(json.error || 'Request failed'); return json; }
function publicFilePath(file) { const ext = String(file.originalName || '').match(/\.[a-z0-9]{1,10}$/i)?.[0]?.toLowerCase() || ''; return `/f/${file.id}${ext}`; }
function empty(text) { return `<div class="empty-state">${escapeHtml(text)}</div>`; }
function skeletonCards(count) { return Array.from({ length: count }, () => '<article class="data-card skeleton-card"><div class="skeleton-check"></div><div><span></span><p></p><small></small></div><div class="card-actions"><b></b><b></b></div></article>').join(''); }
function skeletonAudit(count) { return Array.from({ length: count }, () => '<article class="audit-card skeleton-card"><div><span></span><p></p></div><div><span></span><p></p><small></small></div><div><span></span><p></p></div></article>').join(''); }
function skeletonFields(count) { return Array.from({ length: count }, () => '<label class="skeleton-field"><span></span><i></i></label>').join(''); }
function pagerHTML(showing, total, scope) { return total > showing ? `<span>${escapeHtml(L.showing)} ${showing}/${total}</span><button class="pixel-button secondary compact" type="button" data-load-more="${scope}">${escapeHtml(L.loadMore)}</button>` : `<span>${escapeHtml(L.showing)} ${showing}/${total}</span>`; }
function iconSvg(name) { return `<svg class="ui-icon" viewBox="0 0 24 24" role="img" aria-hidden="true">${iconPaths[name] || iconPaths.files}</svg>`; }
function parseRegionPolicy(value) {
  const raw = String(value || 'global').trim();
  const match = raw.match(/^(allow|deny):(.+)$/i);
  if (!match) return { mode: 'global', codes: [] };
  return { mode: match[1].toLowerCase(), codes: normalizeRegionCodes(match[2]) };
}
function formatRegionPolicy(mode, codes) {
  const clean = normalizeRegionCodes(codes);
  if (mode !== 'allow' && mode !== 'deny') return 'global';
  return clean.length ? `${mode}:${clean.join(',')}` : 'global';
}
function normalizeRegionCodes(value) {
  const list = Array.isArray(value) ? value : String(value || '').split(/[\s,;，；]+/);
  const seen = new Set();
  const out = [];
  list.forEach((item) => {
    const code = String(item || '').trim().toUpperCase().replace(/[^A-Z0-9-]/g, '');
    if (!code || seen.has(code)) return;
    seen.add(code);
    out.push(code);
  });
  return out.slice(0, 32);
}
function updateRegionPolicy(root) {
  if (!root) return;
  const mode = root.querySelector('[data-region-mode]:checked')?.value || 'global';
  const editor = root.querySelector('.region-editor');
  if (editor) editor.hidden = mode === 'global';
  const codesInput = root.querySelector('[data-region-codes]');
  const valueInput = root.querySelector('[data-region-value]');
  const codes = normalizeRegionCodes(codesInput?.value || '');
  if (codesInput) codesInput.value = codes.join(', ');
  if (valueInput) valueInput.value = formatRegionPolicy(mode, codes);
}
function toggleRegionChip(root, code) {
  if (!root || !code) return;
  const input = root.querySelector('[data-region-codes]');
  const mode = root.querySelector('[data-region-mode]:checked')?.value || 'global';
  if (mode === 'global') root.querySelector('[data-region-mode][value="allow"]').checked = true;
  const codes = normalizeRegionCodes(input?.value || '');
  const normalized = normalizeRegionCodes([code])[0];
  const next = codes.includes(normalized) ? codes.filter((item) => item !== normalized) : [...codes, normalized];
  if (input) input.value = next.join(', ');
  updateRegionPolicy(root);
}
function formatSize(n) { n = Number(n || 0); if (n > 1048576) return (n / 1048576).toFixed(1) + ' MiB'; if (n > 1024) return (n / 1024).toFixed(1) + ' KiB'; return n + ' B'; }
function formatTime(value) { const d = new Date(value); return Number.isNaN(d.getTime()) ? value : d.toLocaleString(lang, { hour12: false }); }
function debounce(fn, ms) { let timer; return (...args) => { clearTimeout(timer); timer = setTimeout(() => fn(...args), ms); }; }
function escapeHtml(value) { return String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[char]); }
function escapeAttr(value) { return escapeHtml(value).replace(/`/g, '&#096;'); }
