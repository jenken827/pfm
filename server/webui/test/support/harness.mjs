// 渲染测试的公共环境：装好浏览器全局（document / window / fetch / 定时器），
// 用一张"接口 → 返回数据"的表替掉网络，视图代码本身一行都不用改。
//
// 用法：
//   const env = install({ '/api/file/list': { files: [...] } });
//   await renderView(renderFiles, { params: {}, query: new URLSearchParams() });
//   env.text()          // 整页可见文本
//   env.calls           // 请求过的接口路径（按顺序）

// 必须第一个导入：store.js 在模块求值时就读 localStorage（见 bootstrap.mjs 注释）
import './bootstrap.mjs';
import { FakeDocument, FakeElement, FakeEvent, FakeNode, FakeText } from './fake_dom.mjs';
// session.js 在模块加载时不碰 localStorage（只在方法里用），可以静态导入
import { session } from '../../js/session.js';
import { ensureSpace } from '../../js/store.js';
import { start as installRouter } from '../../js/router.js';

export const SUCCESS = (data) => ({ code: 'SUCCESS', message: 'success', data: data || {} });
export const FAIL = (code, message) => ({ $error: { code, message } });
// 不是 BaseResp 信封的原样返回（登录接口就是裸 JSON：{token, name, isAdmin...}）
export const RAW = (body) => ({ $raw: body });
export const HTTP = (status, body) => ({ $error: { status, body } });

const DEFAULT_ROUTES = {
  '/api/space/get': {
    space: { rootFileId: 'root-1', usedBytes: '1024', quotaBytes: '0', boundProviderIds: '[1]' },
  },
  '/api/space/my-providers': {
    providers: [{
      id: 1, name: '本地', driverId: 'local', description: '', enabled: true,
      defaultBound: true, sortOrder: 0, totalCapacity: '0', usedCapacity: '1024',
      fileSizeLimit: '0', maxConcurrentUploads: 0, proxy: '',
    }],
  },
};

function visibleText(node) {
  if (node.nodeType === 3) return node.data;
  if (node.hidden) return '';
  return node.childNodes.map(visibleText).join('');
}

class Env {
  constructor(routes) {
    this.calls = [];
    this.routes = { ...DEFAULT_ROUTES, ...routes };
    this.requests = [];
  }

  /** 整页可见文本（折叠空白、跳过 hidden 节点，方便一句话断言） */
  text(node = this.document.body) {
    return visibleText(node).replace(/\s+/g, ' ').trim();
  }

  /** 容器里所有某选择器的文本 */
  texts(selector, root = this.document.body) {
    return root.querySelectorAll(selector).map((n) => n.textContent.replace(/\s+/g, ' ').trim());
  }

  find(selector, root = this.document.body) {
    return root.querySelector(selector);
  }

  /** 点一个按钮（按可见文案匹配） */
  clickButton(label, root = this.document.body) {
    const btn = root.querySelectorAll('button').find((b) => b.textContent.trim() === label);
    if (!btn) throw new Error(`找不到按钮「${label}」，页面上有：${root.querySelectorAll('button').map((b) => b.textContent.trim()).join(' / ')}`);
    btn.click();
    return btn;
  }

  /** 让已排队的 0ms 定时器跑掉（openMenu 的延迟绑定、输入框 focus） */
  async settle() {
    for (let i = 0; i < 8; i += 1) {
      await Promise.resolve();
      await new Promise((r) => setImmediate(r));
    }
    // 真定时器：setTimeout(0) 在 timers 阶段执行，比 setImmediate 晚，这里等一小段
    await new Promise((r) => setTimeout(r, 8));
    for (let i = 0; i < 8; i += 1) {
      await Promise.resolve();
      await new Promise((r) => setImmediate(r));
    }
  }
}

/**
 * 安装全局环境。
 * @param {Record<string, any>} routes 接口路径 → 数据（形如 SUCCESS / FAIL 的包装也行）
 */
export function install(routes = {}, { token = 'test-token', user = { name: '测试', isAdmin: true } } = {}) {
  const document = new FakeDocument();
  const overlays = new FakeElement('div');
  overlays.id = 'overlays';
  const toasts = new FakeElement('div');
  toasts.id = 'toasts';
  const root = new FakeElement('div');
  root.id = 'app';
  document.body.appendChild(root);
  document.body.appendChild(overlays);
  document.body.appendChild(toasts);

  const env = new Env(routes);
  env.document = document;
  env.root = root;
  env.overlays = overlays;
  env.toasts = toasts;

  const storage = new Map();
  const localStorage = {
    getItem: (k) => (storage.has(k) ? storage.get(k) : null),
    setItem: (k, v) => storage.set(k, String(v)),
    removeItem: (k) => storage.delete(k),
    clear: () => storage.clear(),
  };
  if (token) storage.set('pfm_web_token', token);
  if (user) storage.set('pfm_web_user', JSON.stringify(user));

  const listeners = new Map();
  let hash = '#/files';
  const location = {
    origin: 'http://example.test',
    href: 'http://example.test/web/',
    pathname: '/web/',
  };
  // 真实浏览器里给 location.hash 赋值会触发 hashchange，路由靠它工作；
  // 测试里必须照做，否则 navigate() 之后什么都不会重新渲染。
  const setHash = (value, dispatch) => {
    const next = String(value);
    if (next === hash) return;
    hash = next;
    if (dispatch) globalThis.window.dispatchEvent(new globalThis.HashChangeEvent('hashchange'));
  };
  Object.defineProperty(location, 'hash', {
    get: () => hash,
    set: (v) => setHash(v, true),
    configurable: true,
  });

  const window = {
    innerWidth: 1280,
    innerHeight: 820,
    location,
    localStorage,
    navigator: { clipboard: null, userAgent: 'node-test' },
    addEventListener: (type, fn) => {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    removeEventListener: (type, fn) => {
      listeners.set(type, (listeners.get(type) || []).filter((f) => f !== fn));
    },
    dispatchEvent: (ev) => {
      (listeners.get(ev.type) || []).slice().forEach((fn) => fn(ev));
      return true;
    },
    open: () => null,
    history: {
      length: 1,
      back() {},
      // router.navigate({replace:true}) 用 replaceState 改地址、再自己派发一次 hashchange
      pushState(_state, _title, url) { setHash(url, true); },
      replaceState(_state, _title, url) { setHash(url, false); },
    },
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
  };

  const g = globalThis;
  // node 24 自带只读的 navigator / location，必须用 defineProperty 覆盖
  const define = (name, value) => Object.defineProperty(g, name, {
    value, configurable: true, writable: true, enumerable: true,
  });
  g.window = window;
  g.document = document;
  define('localStorage', localStorage);
  define('navigator', window.navigator);
  define('location', window.location);
  g.history = window.history;
  env.window = window;
  env.location = window.location;
  g.Node = FakeNode;
  g.HTMLElement = FakeElement;
  g.Event = FakeEvent;
  g.CustomEvent = FakeEvent;
  g.HashChangeEvent = FakeEvent;
  // 定时器保持 node 原生实现：替换掉会破坏 node:test 自己的 IPC / 报告（多文件时挂住）
  g.requestAnimationFrame = (fn) => setTimeout(() => fn(Date.now()), 0);
  g.cancelAnimationFrame = (handle) => clearTimeout(handle);
  g.getComputedStyle = () => ({ getPropertyValue: () => '' });

  g.EventSource = class {};
  g.MutationObserver = class { observe() {} disconnect() {} };

  g.fetch = async (path, init = {}) => {
    const url = String(path);
    env.calls.push(url);
    env.requests.push({ path: url, body: init.body ? JSON.parse(init.body) : null, headers: init.headers || {} });
    let entry = env.routes[url];
    if (entry === undefined) {
      const key = Object.keys(env.routes).find((k) => k.endsWith('*') && url.startsWith(k.slice(0, -1)));
      if (key) entry = env.routes[key];
    }
    if (entry === undefined) {
      throw new Error(`测试缺少接口桩：${url}（已配置：${Object.keys(env.routes).join(', ')}）`);
    }
    if (typeof entry === 'function') entry = entry(env.requests[env.requests.length - 1]);
    if (entry && entry.$raw) return response(200, entry.$raw);
    // 二进制响应（文本预览 / 缩略图）：$blob 可以是字符串或 Uint8Array
    if (entry && entry.$blob !== undefined) {
      const bytes = typeof entry.$blob === 'string'
        ? new TextEncoder().encode(entry.$blob)
        : entry.$blob;
      return {
        status: 200,
        ok: true,
        json: async () => { throw new Error('不是 JSON'); },
        text: async () => new TextDecoder().decode(bytes),
        blob: async () => ({
          size: bytes.length,
          type: entry.$type || 'application/octet-stream',
          arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
          text: async () => new TextDecoder().decode(bytes),
        }),
        headers: { get: () => null },
      };
    }
    // 支持 Range 的媒体桩：$ranged 给出总长度，按请求头切出对应字节（预取用）
    if (entry && entry.$ranged) {
      return rangedResponse(entry.$ranged.total, init.headers);
    }
    if (entry && entry.$error) {
      const { status = 200, code, message, body } = entry.$error;
      const payload = body || { code, message };
      return response(status, payload);
    }
    return response(200, entry && entry.code ? entry : SUCCESS(entry));
  };

  // 视图里的管理员页读 session.user.isAdmin，这里按 localStorage 装载一次会话
  session.load();

  return env;
}

/**
 * 造一个支持 Range 的 206 响应：给预取用的假媒体。
 * 字节内容按位置填充（同偏移取模），足够断言「拿到的长度与偏移正确」。
 */
function rangedResponse(total, headers = {}) {
  const raw = headers.Range || headers.range || 'bytes=0-0';
  const match = /bytes=(\d+)-(\d*)/.exec(String(raw));
  const start = match ? Number(match[1]) : 0;
  const asked = match && match[2] ? Number(match[2]) : total - 1;
  const end = Math.min(asked, total - 1);
  const bytes = new Uint8Array(Math.max(0, end - start + 1));
  for (let i = 0; i < bytes.length; i += 1) bytes[i] = (start + i) % 251;
  return {
    status: 206,
    ok: true,
    json: async () => { throw new Error('不是 JSON'); },
    text: async () => '',
    arrayBuffer: async () => bytes.buffer,
    blob: async () => ({
      size: bytes.length,
      type: 'video/mp4',
      arrayBuffer: async () => bytes.buffer,
    }),
    headers: {
      get: (name) => (String(name).toLowerCase() === 'content-range'
        ? `bytes ${start}-${end}/${total}`
        : null),
    },
  };
}

function response(status, payload) {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => payload,
    text: async () => JSON.stringify(payload),
    blob: async () => ({ size: 0, type: '' }),
    headers: { get: () => null },
  };
}

/**
 * 装一个"迷你外壳"：跟 app.js 的 dispatch 一样，监听 hashchange、解析 hash、渲染对应视图。
 *
 * 只调 renderView() 时，navigate() 改 hash 不会重新渲染，测不出"跳转之后页面空白"这类
 * 问题（历史缺陷：视图的清理回调把新视图的 renderToken 一并作废）。跨路由的用例用它。
 */
export function mountShell(env, views, startHash = '#/files') {
  const main = new FakeElement('main');
  main.className = 'main';
  env.root.replaceChildren(main);
  let current = null;

  const handler = async (route) => {
    const view = views[route.name] || views.files;
    if (!view) throw new Error(`迷你外壳没有注册视图：${route.name}`);
    current = route;
    main.replaceChildren();
    await view(main, route);
  };
  env.shell = { main, handler, route: () => current };
  installRouter(handler);
  env.window.location.hash = startHash;
  return env.shell;
}

/**
 * 渲染一个视图到 #app 里，并等待异步渲染结束。
 *
 * 先 ensureSpace()：真实外壳（app.js 的 dispatch）在渲染任何视图前都会加载用户空间，
 * 视图里 store.rootId 已经可用（目录设置的"根目录"、备份任务的默认目标目录都靠它）。
 */
export async function renderView(env, render, route = {}) {
  const main = new FakeElement('main');
  main.className = 'main';
  env.root.replaceChildren(main);
  try {
    await ensureSpace();
  } catch {
    // 没有空间的账号由外壳统一提示，这里只关心视图本身
  }
  const fullRoute = {
    params: {},
    query: new URLSearchParams(''),
    path: '/files',
    ...route,
  };
  await render(main, fullRoute);
  await env.settle();
  return main;
}
