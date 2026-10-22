// 预览页：与客户端 `preview_router.dart` 同一套分流规则。
//
//   图片            → 本页（支持缩放 / 拖动 / 同目录左右翻页）
//   视频 / 音频     → 本页 <video>/<audio>（Range + 票据，可拖动进度）
//   纯文本          → 本页（<1MB、BOM 嗅探、Markdown 渲染 / 源码切换）
//   Office/PDF/压缩包 → Flyfish Viewer（inline v3，退 iframe v1，退下载）
//   其余            → 详情页
//
// 凭证：一律单文件票据，URL 里不出现 access token。媒体与文档用 12 小时下载票
// （播放器会续传/拖动，10 分钟会中途失效），文本与缩略图用 10 分钟预览票。

import {
  call, ticket, rawPreviewUrl, previewPath, fetchBlob, downloadUrl,
  PURPOSE_DOWNLOAD, buildDownloadUrl,
} from '../api.js';
import { h, toast, triggerDownload, setChildren } from '../dom.js';
import { fmtSize, iconFor, typeText } from '../format.js';
import { navigate, back } from '../router.js';
import { store } from '../store.js';
import {
  decodeTextBytes, isMarkdownFile, renderMarkdown, TEXT_MAX_BYTES,
} from '../text_preview.js';
import { previewKindOf } from './preview_kind.js';
import { mountViewer, unmountViewer } from './viewer.js';

let renderToken = 0;

export async function renderPreview(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;
  unmountViewer();

  main.className = 'main flush';
  setChildren(main, h('div', { class: 'empty' }, h('div', { text: '正在打开…' })));

  let file;
  try {
    file = await call('/api/file/info', { fileId: route.params.id });
  } catch (err) {
    if (alive()) setChildren(main, renderBroken(`打开失败：${err.message}`, route.params.id));
    return;
  }
  if (!alive()) return;

  const kind = previewKindOf(file);
  if (kind === 'detail') {
    navigate(`/detail/${encodeURIComponent(file.id)}`, { replace: true });
    return;
  }

  const stage = h('div', { class: 'preview-stage' });
  const shell = h('div', { class: 'preview-shell' },
    buildBar(file, kind, route, alive),
    stage);
  setChildren(main, shell);

  try {
    if (kind === 'image') await renderImage(stage, file, route, alive);
    else if (kind === 'media') await renderMedia(stage, file, alive);
    else if (kind === 'text') await renderText(stage, file, alive);
    else await renderDocument(stage, file, alive);
  } catch (err) {
    if (!alive()) return;
    setChildren(stage, h('div', { class: 'empty' },
      h('span', { class: 'big', text: '⚠️' }),
      h('div', { class: 'error-text', text: `预览失败：${err.message}` }),
      h('div', { style: { marginTop: '12px' } },
        h('button', { text: '重试', onclick: () => renderPreview(main, route) }))));
  }

  const onKey = (ev) => {
    if (!alive()) return;
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') return;
    if (ev.key === 'Escape') back(`/files/${encodeURIComponent(file.parentId || store.rootId)}`);
  };
  window.addEventListener('keydown', onKey);
  window.addEventListener('hashchange', () => {
    unmountViewer();
    window.removeEventListener('keydown', onKey);
  }, { once: true });
}

function buildBar(file, kind, route, alive) {
  const downloadBtn = h('button', {
    class: 'primary',
    text: '下载',
    onclick: async () => {
      try {
        const url = await downloadUrl(file.id);
        if (alive()) triggerDownload(url);
      } catch (err) {
        toast(`下载失败：${err.message}`, 'err');
      }
    },
  });
  const backTarget = route.query.get('from')
    ? `/files/${encodeURIComponent(route.query.get('from'))}`
    : `/files/${encodeURIComponent(file.parentId || store.rootId)}`;

  return h('div', { class: 'preview-bar' },
    h('button', { class: 'ghost', text: '← 返回', onclick: () => back(backTarget) }),
    h('span', { class: 'title', text: file.name || '(未命名)', title: file.name || '' }),
    h('span', { class: 'muted small meta', text: `${iconFor(file)} ${typeText(file.type)} · ${fmtSize(file.size)}` }),
    h('div', { class: 'spacer' }),
    kind === 'text' ? h('span', { class: 'muted small tag', text: '文本' }) : null,
    h('a', { class: 'muted small detail-link', href: `#/detail/${encodeURIComponent(file.id)}`, text: '详情' }),
    downloadBtn);
}

// ---------------------------------------------------------------- 图片

async function renderImage(stage, file, route, alive) {
  const { ticket: t } = await ticket(file.id, PURPOSE_DOWNLOAD);
  const src = rawPreviewUrl(file.id, file.name, t);

  const img = h('img', { class: 'zoomable', src, alt: file.name || '' });
  const hint = h('div', { class: 'muted small', style: { position: 'absolute', bottom: '10px', left: '50%', transform: 'translateX(-50%)' }, text: '滚轮缩放 · 拖动平移 · 双击复位' });
  setChildren(stage, img, hint);
  if (!alive()) return;

  const zoom = attachZoom(img);

  // 同目录图集：只在"从目录进来"时提供左右翻页
  const from = route.query.get('from');
  if (!from) return;
  let images = [];
  try {
    const data = await call('/api/file/list', { parentId: from, offset: 0, pageSize: 500 });
    images = (data.files || []).filter((f) => f.type === 'IMAGE');
  } catch {
    return; // 图集只是增强，取不到就单张看
  }
  if (!alive() || images.length < 2) return;

  let index = images.findIndex((f) => f.id === file.id);
  if (index < 0) return;

  const counter = h('span', { class: 'muted small' });
  const prev = h('button', { class: 'preview-nav prev', text: '‹', title: '上一张（←）', onclick: () => go(-1) });
  const next = h('button', { class: 'preview-nav next', text: '›', title: '下一张（→）', onclick: () => go(1) });
  stage.append(prev, next, h('span', { class: 'muted small', style: { position: 'absolute', top: '10px', right: '14px' } }, counter));

  async function go(delta) {
    const target = index + delta;
    if (target < 0 || target >= images.length) return;
    index = target;
    try {
      const { ticket: nt } = await ticket(images[index].id, PURPOSE_DOWNLOAD);
      if (!alive()) return;
      img.src = rawPreviewUrl(images[index].id, images[index].name, nt);
      zoom.reset();
      update();
    } catch (err) {
      toast(`切换失败：${err.message}`, 'err');
    }
  }

  function update() {
    counter.textContent = `${index + 1} / ${images.length}`;
    prev.disabled = index === 0;
    next.disabled = index === images.length - 1;
  }
  update();

  const onKey = (ev) => {
    if (!alive()) return;
    if (ev.key === 'ArrowLeft') go(-1);
    if (ev.key === 'ArrowRight') go(1);
  };
  window.addEventListener('keydown', onKey);
}

/** 缩放 / 平移：wheel 以光标为中心缩放，拖动平移，双击在 1x 与 2.5x 之间切换 */
function attachZoom(img) {
  const state = { scale: 1, x: 0, y: 0, dragging: false, sx: 0, sy: 0, ox: 0, oy: 0 };

  const apply = () => {
    const clamped = Math.min(Math.max(state.scale, 1), 8);
    state.scale = clamped;
    if (clamped === 1) {
      state.x = 0;
      state.y = 0;
    }
    img.style.transform = clamped === 1
      ? ''
      : `translate(${state.x}px, ${state.y}px) scale(${clamped})`;
    img.classList.toggle('zoomed', clamped > 1);
    img.style.cursor = clamped > 1 ? 'move' : 'grab';
  };

  img.addEventListener('wheel', (ev) => {
    ev.preventDefault();
    const rect = img.getBoundingClientRect();
    const cx = ev.clientX - (rect.left + rect.width / 2);
    const cy = ev.clientY - (rect.top + rect.height / 2);
    const prevScale = state.scale;
    const factor = ev.deltaY < 0 ? 1.15 : 1 / 1.15;
    state.scale = Math.min(Math.max(prevScale * factor, 1), 8);
    // 让光标下的那个点尽量不动
    const ratio = state.scale / prevScale;
    state.x = (state.x - cx) * ratio + cx;
    state.y = (state.y - cy) * ratio + cy;
    apply();
  }, { passive: false });

  img.addEventListener('pointerdown', (ev) => {
    if (state.scale <= 1) return;
    state.dragging = true;
    state.sx = ev.clientX;
    state.sy = ev.clientY;
    state.ox = state.x;
    state.oy = state.y;
    img.setPointerCapture(ev.pointerId);
  });
  img.addEventListener('pointermove', (ev) => {
    if (!state.dragging) return;
    state.x = state.ox + (ev.clientX - state.sx);
    state.y = state.oy + (ev.clientY - state.sy);
    apply();
  });
  const stop = () => { state.dragging = false; };
  img.addEventListener('pointerup', stop);
  img.addEventListener('pointercancel', stop);

  const toggle = () => {
    state.scale = state.scale > 1 ? 1 : 2.5;
    apply();
  };
  img.addEventListener('dblclick', toggle);

  return {
    reset: () => {
      state.scale = 1;
      apply();
    },
  };
}

// ---------------------------------------------------------------- 音视频

async function renderMedia(stage, file, alive) {
  const { ticket: t } = await ticket(file.id, PURPOSE_DOWNLOAD);
  const src = rawPreviewUrl(file.id, file.name, t);

  const status = h('div', { class: 'muted small' });
  const box = h('div', { class: 'media-center' });
  setChildren(stage, box);

  const el = file.type === 'VIDEO'
    ? h('video', { controls: true, preload: 'metadata', playsinline: '' })
    : h('audio', { controls: true, preload: 'metadata' });
  box.append(el, status);

  el.addEventListener('error', () => {
    status.textContent = '播放失败：文件可能尚未就绪，或凭证已失效（重新打开本页即可换新票据）';
  });

  // Telegram 大文件首次访问会返回 202「正在缓存」：先探一次，避免 <video> 直接
  // 拿到 JSON 当成媒体流解析（表现为一直转圈）。
  status.textContent = '正在准备…';
  const ready = await waitReady(src, status, alive);
  if (!alive()) return;
  if (!ready) {
    status.textContent = '文件仍在云端准备，稍后重新打开本页即可播放';
    return;
  }
  status.textContent = '';
  el.src = src;
  box.prepend(el);
}

/** 轮询直到可播放（200/206）；返回 false 表示超时 */
async function waitReady(url, status, alive, { tries = 100, interval = 3000 } = {}) {
  for (let i = 0; i < tries; i += 1) {
    if (!alive()) return false;
    try {
      const res = await fetch(url, { headers: { Range: 'bytes=0-0' }, cache: 'no-store' });
      if (res.status === 200 || res.status === 206) {
        res.body?.cancel?.();
        return true;
      }
      res.body?.cancel?.();
      if (res.status === 202) {
        status.textContent = `云端正在准备该文件（${i + 1}）…`;
      } else if (res.status === 401 || res.status === 403) {
        status.textContent = '凭证已失效，请重新打开本页';
        return false;
      } else {
        status.textContent = `读取失败（HTTP ${res.status}）`;
        return false;
      }
    } catch {
      status.textContent = '网络中断，正在重试…';
    }
    await new Promise((r) => setTimeout(r, interval));
  }
  return false;
}

// ---------------------------------------------------------------- 纯文本

async function renderText(stage, file, alive) {
  const path = previewPath(file.id, { original: true });
  const blob = await fetchBlob(path, { range: `bytes=0-${TEXT_MAX_BYTES - 1}` });
  if (!alive()) return;
  const bytes = new Uint8Array(await blob.arrayBuffer());
  const decoded = decodeTextBytes(bytes);

  const pane = h('div', { class: 'text-pane' });
  setChildren(stage, pane);

  const markdown = isMarkdownFile(file.name);
  let mode = markdown ? 'render' : 'source';

  const toggle = markdown
    ? h('div', { class: 'seg', style: { position: 'absolute', top: '10px', right: '14px', zIndex: '4' } })
    : null;

  const render = () => {
    setChildren(pane);
    if (mode === 'render') {
      const md = h('div', { class: 'markdown' });
      // renderMarkdown 内部已对原文做 HTML 转义，只输出自己生成的标签
      md.innerHTML = renderMarkdown(decoded.content);
      pane.appendChild(md);
    } else {
      pane.appendChild(h('pre', { text: decoded.content }));
    }
    if (decoded.truncated) {
      pane.appendChild(h('div', { class: 'hint', text: `文件超过 ${fmtSize(TEXT_MAX_BYTES)}，仅显示前 ${fmtSize(TEXT_MAX_BYTES)}` }));
    }
    if (decoded.invalidUtf8) {
      pane.appendChild(h('div', { class: 'hint', text: '文件不是合法的 UTF-8，部分字符可能显示为乱码' }));
    }
  };

  if (toggle) {
    const btn = (value, label) => h('button', {
      class: mode === value ? 'on' : '',
      text: label,
      onclick: (ev) => {
        mode = value;
        toggle.querySelectorAll('button').forEach((b) => b.classList.remove('on'));
        ev.target.classList.add('on');
        render();
      },
    });
    toggle.append(btn('render', '渲染'), btn('source', '源码'));
    stage.appendChild(toggle);
  }
  render();
}

// ---------------------------------------------------------------- 文档 / 压缩包

async function renderDocument(stage, file, alive) {
  const { ticket: t } = await ticket(file.id, PURPOSE_DOWNLOAD);
  const url = rawPreviewUrl(file.id, file.name, t);
  const result = await mountViewer(stage, file, url);
  if (!alive()) return;
  if (result) return;

  // 没有 Viewer 资源：直接给下载入口（票据已在手，12 小时有效）
  setChildren(stage, h('div', { class: 'viewer-hint' },
    h('div', { class: 'muted', text: '当前部署未提供文档预览器资源，可以下载后用本地应用打开。' }),
    h('div', { style: { marginTop: '14px' } },
      h('button', {
        class: 'primary',
        text: `下载 ${file.name || '文件'}`,
        onclick: () => triggerDownload(buildDownloadUrl(file.id, t)),
      }))));
}

function renderBroken(message, fileId) {
  return h('div', { class: 'empty' },
    h('span', { class: 'big', text: '⚠️' }),
    h('div', { class: 'error-text', text: message }),
    h('div', { style: { marginTop: '12px' }, class: 'toolbar' },
      h('button', { text: '返回', onclick: () => back('/files') }),
      h('button', { text: '看详情', onclick: () => navigate(`/detail/${encodeURIComponent(fileId)}`) })));
}
