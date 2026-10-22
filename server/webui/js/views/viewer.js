// Office / PDF / 压缩包预览：优先在页面内 inline mount Flyfish Viewer v3，
// 资源不存在时退回 v1 的 iframe 页，再退回"直接下载"。
//
// 为什么要探测资源：Viewer 的分发包（renderers / wasm / vendor，上百 MB）不可能
// 进仓库，也不一定每个部署都放了。探测失败不是错误，只是换一种可用的降级路径。
//
// v3 用 Shadow DOM 挂载（与宿主页面样式隔离），入口是 IIFE + window 全局，
// 不需要打包器，符合"Web UI 只用原生 ES module"的约束。

import { rawPreviewUrl } from '../api.js';
import { h, spinner, toast, setChildren } from '../dom.js';

const V3_BASE = '/file-viewer-v3/';
const V3_SCRIPT = `${V3_BASE}flyfish-file-viewer-web-full.iife.js`;
const V3_MANIFEST = `${V3_BASE}flyfish-viewer-manifest.json`;
const V1_INDEX = '/file-viewer/index.html';

const probeCache = new Map();
let v3ScriptPromise = null;

/** HEAD 探测资源是否存在（结果缓存：一次会话只探一次） */
function probe(url) {
  if (probeCache.has(url)) return probeCache.get(url);
  const promise = fetch(url, { method: 'HEAD', cache: 'no-store' })
    .then((res) => res.ok)
    .catch(() => false);
  probeCache.set(url, promise);
  return promise;
}

export async function v3Available() {
  return (await probe(V3_MANIFEST)) || (await probe(V3_SCRIPT));
}

export async function v1Available() {
  return probe(V1_INDEX);
}

/** 动态加载 v3 IIFE（只加载一次），返回 window 全局命名空间 */
function loadV3Script() {
  if (window.FlyfishFileViewerWebFull?.mountViewer) return Promise.resolve(window.FlyfishFileViewerWebFull);
  if (v3ScriptPromise) return v3ScriptPromise;

  v3ScriptPromise = new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = V3_SCRIPT;
    script.async = true;
    script.onload = () => {
      const api = window.FlyfishFileViewerWebFull;
      if (!api?.mountViewer) {
        reject(new Error('Viewer 脚本已加载但未导出 mountViewer'));
        return;
      }
      resolve(api);
    };
    script.onerror = () => reject(new Error('Viewer 脚本加载失败'));
    document.head.appendChild(script);
  }).catch((err) => {
    v3ScriptPromise = null;
    throw err;
  });
  return v3ScriptPromise;
}

let mounted = null;

/** 卸载当前 inline viewer（路由切换时调用） */
export function unmountViewer() {
  if (!mounted) return;
  try {
    mounted.destroy();
  } catch {
    /* 卸载失败不影响切换页面 */
  }
  mounted = null;
}

/**
 * 在容器里挂载 Viewer。
 *
 * @param {HTMLElement} container
 * @param {{id: string, name: string}} file
 * @param {string} url 已带票据的原始文件地址
 * @returns {Promise<{mode: 'v3'|'v1'}|null>} null 表示没有可用的 Viewer（调用方给下载入口）
 */
export async function mountViewer(container, file, url) {
  unmountViewer();

  if (await v3Available()) {
    const mode = await mountV3(container, file, url);
    if (mode) return { mode };
  }
  if (await v1Available()) {
    mountV1(container, url);
    return { mode: 'v1' };
  }
  return null;
}

async function mountV3(container, file, url) {
  setChildren(container, h('div', { class: 'empty' }, spinner(), h('div', { text: '正在加载预览器…' })));
  const api = await loadV3Script();
  // 资源基址必须是绝对 URL：Viewer 内部用 new URL('renderers/…', base) 拼
  api.setDefaultFullAssetBaseUrl(new URL(V3_BASE, location.origin).href);

  const host = h('div', { class: 'viewer-host' });
  setChildren(container, host);

  try {
    const controller = api.mountViewer(host, {
      url,
      filename: file.name,
      options: { theme: 'system', toolbar: true, styleIsolation: 'shadow' },
    });
    mounted = {
      destroy: () => {
        controller?.destroy?.();
        setChildren(host);
      },
    };
    return 'v3';
  } catch (err) {
    setChildren(host);
    toast(`内置预览器启动失败：${err.message}`, 'err');
    return '';
  }
}

function mountV1(container, url) {
  // v1 页面从 ?url= 读文件地址（与客户端 WebView 的约定一致）
  const src = `${V1_INDEX}?url=${encodeURIComponent(url)}&options=${encodeURIComponent('{"toolbar":true}')}`;
  const frame = h('iframe', { src, title: '文件预览', referrerpolicy: 'no-referrer' });
  setChildren(container, h('div', { class: 'viewer-host' }, frame));
  mounted = { destroy: () => frame.remove() };
}

/** 没有 Viewer 时的兜底：说明 + 下载按钮 */
export function renderViewerUnavailable(container, file, downloadHref) {
  setChildren(container, h('div', { class: 'viewer-hint' },
    h('div', { class: 'muted', text: '当前部署没有提供文档预览器资源，可以直接下载后用本地应用打开。' }),
    h('div', { style: { marginTop: '14px' } },
      h('a', { class: 'download-btn', href: downloadHref, text: `下载 ${file.name || '文件'}` }))));
}
