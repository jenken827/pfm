// Web UI 入口：外壳（顶栏 / 侧边栏）+ 路由分发。
//
// 没有框架、没有打包器：所有视图都是一个 render(main, route) 函数，路由变化时
// 清空 main 重新渲染。视图之间通过 hash 链接互相跳转，共享状态只有 store 里的
// 根目录 / 存储源 / 偏好。

import { session } from './session.js';
import { h, $, toast, setChildren } from './dom.js';
import { navigate, start, parse } from './router.js';
import { ensureSpace, refreshSpace, store } from './store.js';
import { cycleTheme, themeIcon, themeLabel } from './theme.js';
import { fmtSize, fmtSize0 } from './format.js';
import { renderLogin } from './views/login.js';
import { renderFiles } from './views/files.js';
import { renderPreview } from './views/preview.js';
import { renderDetail } from './views/detail.js';
import { renderTrash } from './views/trash.js';
import { renderShares } from './views/shares.js';
import { renderOffline } from './views/offline.js';
import { renderTransfers } from './views/transfers.js';
import { renderWebdav } from './views/webdav.js';
import { renderMcp } from './views/mcp.js';
import { renderAdminUsers } from './views/admin_users.js';
import { renderAdminProviders } from './views/admin_providers.js';
import { renderAdminBackup } from './views/admin_backup.js';
import { renderSettings } from './views/settings.js';

const VIEWS = {
  files: renderFiles,
  recent: renderFiles,
  type: renderFiles,
  search: renderFiles,
  preview: renderPreview,
  detail: renderDetail,
  trash: renderTrash,
  shares: renderShares,
  offline: renderOffline,
  transfers: renderTransfers,
  webdav: renderWebdav,
  mcp: renderMcp,
  adminUsers: renderAdminUsers,
  adminProviders: renderAdminProviders,
  adminBackup: renderAdminBackup,
  settings: renderSettings,
};

const TYPE_NAV = [
  ['IMAGE', '图片', '🖼️'],
  ['VIDEO', '视频', '🎬'],
  ['AUDIO', '音频', '🎵'],
  ['DOCUMENT', '文档', '📄'],
  ['ARCHIVE', '压缩包', '🗜️'],
  ['APPLICATION', '其他', '📦'],
];

const app = {
  topbar: null,
  sidebar: null,
  main: null,
  current: null,
};

function buildShell() {
  const search = h('input', { type: 'search', placeholder: '搜索文件名…', 'aria-label': '搜索' });
  search.addEventListener('keydown', (ev) => {
    if (ev.key !== 'Enter') return;
    const q = search.value.trim();
    if (q) navigate(`/search?q=${encodeURIComponent(q)}`);
  });

  const user = session.user || {};
  const themeBtn = h('button', {
    class: 'ghost icon',
    text: themeIcon(),
    title: `主题：${themeLabel()}（点击切换）`,
    onclick: () => {
      const mode = cycleTheme();
      themeBtn.textContent = themeIcon(mode);
      themeBtn.title = `主题：${themeLabel(mode)}（点击切换）`;
    },
  });

  const topbar = h('header', { class: 'topbar' },
    h('button', {
      class: 'ghost icon menubtn',
      text: '☰',
      title: '菜单',
      onclick: () => app.sidebar.classList.toggle('open'),
    }),
    h('a', { class: 'brand', href: '#/files' },
      h('span', { class: 'logo', text: '📁' }),
      h('span', { text: 'PFM 文件浏览' })),
    h('div', { class: 'searchbox' },
      h('span', { class: 'lens', text: '🔍' }),
      search),
    h('div', { class: 'spacer' }),
    themeBtn,
    h('div', { class: 'userbox' },
      h('span', { class: 'avatar', text: (user.name || '?').slice(0, 1).toUpperCase(), title: user.name || '' }),
      h('span', { class: 'muted small nowrap', text: user.name || '' }),
      h('button', {
        class: 'sm ghost',
        text: '退出',
        onclick: () => {
          session.clear();
          location.hash = '#/login';
          showLogin();
        },
      })));

  const navItem = (href, icon, label, match) => h('a', {
    class: 'nav-item',
    href,
    dataset: { match },
    onclick: () => app.sidebar.classList.remove('open'),
  }, h('span', { class: 'ico', text: icon }), h('span', { text: label }));

  const sidebar = h('aside', { class: 'sidebar' },
    h('div', { class: 'nav-group' },
      h('div', { class: 'nav-title', text: '浏览' }),
      navItem('#/files', '📁', '我的文件', 'files'),
      navItem('#/recent', '🕘', '最近访问', 'recent')),
    h('div', { class: 'nav-group' },
      h('div', { class: 'nav-title', text: '按类型' }),
      ...TYPE_NAV.map(([type, label, icon]) =>
        navItem(`#/type/${type}`, icon, label, `type:${type}`))),
    h('div', { class: 'nav-group' },
      h('div', { class: 'nav-title', text: '工具' }),
      navItem('#/trash', '🗑️', '回收站', 'trash'),
      navItem('#/shares', '🔗', '我的分享', 'shares'),
      navItem('#/offline', '⬇️', '离线下载', 'offline'),
      navItem('#/transfers', '🔁', '传输队列', 'transfers'),
      navItem('#/webdav', '🔌', 'WebDAV 账号', 'webdav'),
      navItem('#/mcp', '🤖', 'MCP 令牌', 'mcp')));

  // 管理组只对管理员显示（服务端也会再校验一次，这里纯属别让人点进去看 403）
  const adminGroup = h('div', { class: 'nav-group', hidden: true },
    h('div', { class: 'nav-title', text: '管理' }),
    navItem('#/admin/users', '👥', '用户与配额', 'adminUsers'),
    navItem('#/admin/providers', '🗄️', '存储源', 'adminProviders'),
    navItem('#/admin/backup', '🧩', '备份任务', 'adminBackup'));
  sidebar.appendChild(adminGroup);
  app.adminNav = adminGroup;

  // 容量条：和客户端"存储空间"同一个口径（quota 为 0 表示不限）
  const spaceBox = h('div', { class: 'space-box', hidden: true });
  sidebar.appendChild(spaceBox);
  app.spaceBox = spaceBox;

  const renderSpace = () => {
    const space = store.space;
    const quota = Number(space?.quotaBytes || 0);
    const used = Number(space?.usedBytes || 0);
    if (!space) {
      spaceBox.hidden = true;
      return;
    }
    spaceBox.hidden = false;
    const ratio = quota > 0 ? Math.min(1, used / quota) : 0;
    setChildren(spaceBox,
      h('div', { class: 'space-line' },
        h('span', { text: quota > 0 ? '已用' : '已用（不限）' }),
        // fmtSize(0) 返回 '-'（表格里表示"未知"），但容量条里 0 就是真的一点没占，
        // 显示成 "-" 会让人以为没加载出来
        h('span', { class: 'muted', text: quota > 0 ? `${fmtSize0(used)} / ${fmtSize(quota)}` : fmtSize0(used) })),
      quota > 0
        ? h('div', { class: `space-bar${ratio >= 0.95 ? ' full' : ''}` }, h('i', { style: { width: `${(ratio * 100).toFixed(1)}%` } }))
        : null);
  };
  app.renderSpace = renderSpace;
  window.addEventListener('pfm:space', renderSpace);

  const main = h('main', { class: 'main' });
  const shell = h('div', { class: 'shell' }, sidebar, main);

  $('#app').replaceChildren(topbar, shell, h('div', { class: 'login-wrap', hidden: true }), h('div', { id: 'overlays' }), h('div', { id: 'toasts' }));
  app.topbar = topbar;
  app.sidebar = sidebar;
  app.main = main;
}

function syncAdminNav() {
  if (app.adminNav) app.adminNav.hidden = !session.user?.isAdmin;
}

function setActive(route) {
  const key = route.name === 'type' ? `type:${route.params.type}` : route.name;
  app.sidebar.querySelectorAll('.nav-item').forEach((item) => {
    item.classList.toggle('active', item.dataset.match === key);
  });
}

function showLogin() {
  app.topbar.hidden = true;
  app.sidebar.parentElement.hidden = true;
  const wrap = $('.login-wrap');
  wrap.hidden = false;
  renderLogin(wrap, () => {
    wrap.hidden = true;
    app.topbar.hidden = false;
    app.sidebar.parentElement.hidden = false;
    syncAdminNav();
    refreshSpace();
    navigate('/files', { replace: true });
  });
  setChildren(app.main);
}

async function dispatch(route) {
  if (!session.isLoggedIn) {
    showLogin();
    return;
  }
  app.topbar.hidden = false;
  app.sidebar.parentElement.hidden = false;
  $('.login-wrap').hidden = true;
  syncAdminNav();
  setActive(route);

  const view = VIEWS[route.name];
  app.main.className = 'main';
  // 预览页沉浸显示：收起侧边栏，把整屏让给内容
  document.body.classList.toggle('immersive', route.name === 'preview');
  if (!view) {
    navigate('/files', { replace: true });
    return;
  }
  try {
    await ensureSpace();
    if (app.renderSpace) app.renderSpace();
    await view(app.main, route);
  } catch (err) {
    // 账号还没有存储空间（根目录）时说人话，别让人对着"参数不正确"猜
    if (err?.noSpace) {
      setChildren(app.main, h('div', { class: 'empty' },
        h('span', { class: 'big', text: '🗂️' }),
        h('div', { text: '你的账号还没有分配存储空间（根目录）。' }),
        h('div', { class: 'muted small', style: { marginTop: '6px' },
          text: '请联系管理员在「管理 → 用户与配额」里为你分配根目录后再刷新。' }),
        h('div', { style: { marginTop: '12px' } },
          h('button', { text: '重试', onclick: () => dispatch(route) }))));
      return;
    }
    setChildren(app.main, h('div', { class: 'empty' },
      h('span', { class: 'big', text: '⚠️' }),
      h('div', { class: 'error-text', text: err.rawMessage || err.message || String(err) }),
      h('div', { style: { marginTop: '12px' } },
        h('button', { text: '重试', onclick: () => dispatch(route) }))));
  }
}

function boot() {
  session.load();
  buildShell();

  window.addEventListener('pfm:unauthorized', () => {
    showLogin();
    toast('登录已过期，请重新登录', 'err');
  });

  start((route) => {
    // 记录当前路由，方便视图判断"我还在不在页面上"
    app.current = route;
    dispatch(route);
  });
}

boot();

export { app, parse, store };
