// 极简 hash 路由：#/files/<id>、#/preview/<id>?from=<folderId> …
//
// 用 hash 而不是 History API：静态资源由 Go 二进制直出，没有服务端 rewrite，
// hash 天然不会打到后端（刷新任意页面都还是那个 index.html）。

const ROUTES = [
  { name: 'files', re: /^\/files\/([^/]+)$/, keys: ['id'] },
  { name: 'files', re: /^\/files\/?$/, keys: [] },
  { name: 'recent', re: /^\/recent$/, keys: [] },
  { name: 'type', re: /^\/type\/([A-Z_]+)$/, keys: ['type'] },
  { name: 'search', re: /^\/search$/, keys: [] },
  { name: 'preview', re: /^\/preview\/([^/]+)$/, keys: ['id'] },
  { name: 'detail', re: /^\/detail\/([^/]+)$/, keys: ['id'] },
  { name: 'trash', re: /^\/trash$/, keys: [] },
  { name: 'shares', re: /^\/shares$/, keys: [] },
  { name: 'offline', re: /^\/offline$/, keys: [] },
  { name: 'transfers', re: /^\/transfers$/, keys: [] },
  { name: 'webdav', re: /^\/webdav$/, keys: [] },
  { name: 'adminUsers', re: /^\/admin\/users$/, keys: [] },
  { name: 'adminProviders', re: /^\/admin\/providers$/, keys: [] },
  { name: 'adminBackup', re: /^\/admin\/backup$/, keys: [] },
  { name: 'settings', re: /^\/settings\/([^/]+)$/, keys: ['id'] },
  { name: 'login', re: /^\/login$/, keys: [] },
];

/** 解析 hash → {name, params, query, path}；无法识别时回落到 files */
export function parse(hash = location.hash) {
  const raw = String(hash || '').replace(/^#/, '');
  const qIndex = raw.indexOf('?');
  const path = (qIndex < 0 ? raw : raw.slice(0, qIndex)) || '/files';
  const query = new URLSearchParams(qIndex < 0 ? '' : raw.slice(qIndex + 1));

  for (const route of ROUTES) {
    const m = route.re.exec(path);
    if (!m) continue;
    const params = {};
    route.keys.forEach((key, i) => {
      params[key] = decodeURIComponent(m[i + 1] || '');
    });
    return { name: route.name, params, query, path };
  }
  return { name: 'files', params: {}, query, path: '/files' };
}

/** 拼 hash：path 自带 ? 时保留 */
export function href(path, query) {
  const qs = query ? new URLSearchParams(query).toString() : '';
  return `#${path}${qs ? `?${qs}` : ''}`;
}

/** 跳转（同一 hash 时手动触发一次渲染） */
export function navigate(path, { replace = false } = {}) {
  const target = path.startsWith('#') ? path : `#${path}`;
  if (location.hash === target) {
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    return;
  }
  if (replace) {
    history.replaceState(null, '', target);
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    return;
  }
  location.hash = target;
}

/** 返回上一页；没有历史时回退到 fallback */
export function back(fallback = '/files') {
  if (history.length > 1) history.back();
  else navigate(fallback);
}

export function start(handler) {
  window.addEventListener('hashchange', () => handler(parse()));
  handler(parse());
}
