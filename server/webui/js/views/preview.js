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
  call, ticket, rawPreviewUrl, thumbPreviewUrl, previewPath, fetchBlob, downloadUrl,
  PURPOSE_DOWNLOAD, buildDownloadUrl, transcodeUrl,
} from '../api.js';
import { h, toast, triggerDownload, setChildren } from '../dom.js';
import { prefetchMedia } from '../media_prefetch.js';
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

  // 图集翻页后"当前文件"会变：标题 / 详情 / 下载都要跟着走，否则在第 4 张上点
  // 下载拿到的还是第 1 张。
  let current = file;
  const bar = buildBar(() => current, kind, route, alive);

  const stage = h('div', { class: 'preview-stage' });
  const shell = h('div', { class: 'preview-shell' }, bar, stage);
  setChildren(main, shell);

  const onChange = (next) => {
    current = next;
    bar.sync();
    // 不触发 hashchange（会重建整页），只让刷新 / 复制地址停留在当前这张
    const query = route.query.get('from') ? `?from=${encodeURIComponent(route.query.get('from'))}` : '';
    history.replaceState(null, '', `#/preview/${encodeURIComponent(next.id)}${query}`);
  };

  try {
    if (kind === 'image') await renderImage(stage, file, route, alive, onChange);
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

/**
 * 顶部信息条。`getFile` 每次取"当前正在看的文件"——图集翻页后标题 / 详情 / 下载
 * 都要指向新的那张，调用方通过返回元素的 `sync()` 触发刷新。
 */
function buildBar(getFile, kind, route, alive) {
  const file = getFile();
  const downloadBtn = h('button', {
    class: 'primary',
    text: '下载',
    onclick: async () => {
      try {
        const url = await downloadUrl(getFile().id);
        if (alive()) triggerDownload(url);
      } catch (err) {
        toast(`下载失败：${err.message}`, 'err');
      }
    },
  });
  const backTarget = route.query.get('from')
    ? `/files/${encodeURIComponent(route.query.get('from'))}`
    : `/files/${encodeURIComponent(file.parentId || store.rootId)}`;

  const title = h('span', { class: 'title', text: file.name || '(未命名)', title: file.name || '' });
  const meta = h('span', { class: 'muted small meta', text: `${iconFor(file)} ${typeText(file.type)} · ${fmtSize(file.size)}` });
  const detail = h('a', {
    class: 'muted small detail-link',
    href: `#/detail/${encodeURIComponent(file.id)}`,
    text: '详情',
  });

  const bar = h('div', { class: 'preview-bar' },
    h('button', { class: 'ghost', text: '← 返回', onclick: () => back(backTarget) }),
    title,
    meta,
    h('div', { class: 'spacer' }),
    kind === 'text' ? h('span', { class: 'muted small tag', text: '文本' }) : null,
    detail,
    downloadBtn);

  bar.sync = () => {
    const f = getFile();
    title.textContent = f.name || '(未命名)';
    title.title = f.name || '';
    meta.textContent = `${iconFor(f)} ${typeText(f.type)} · ${fmtSize(f.size)}`;
    detail.href = `#/detail/${encodeURIComponent(f.id)}`;
  };
  return bar;
}

// ---------------------------------------------------------------- 图片

async function renderImage(stage, file, route, alive, onChange) {
  const { ticket: t } = await ticket(file.id, PURPOSE_DOWNLOAD);

  // 先显示缩略图（本地 400px JPEG，几乎立刻出图），要看细节再「查看原图」或放大。
  // 原图可能是几 MB 的手机照片，一进预览就拉原图既慢又费流量。
  let originalSrc = rawPreviewUrl(file.id, file.name, t);
  let showingOriginal = false;

  const img = h('img', { class: 'zoomable', src: thumbPreviewUrl(file.id, t, 'large'), alt: file.name || '' });
  const hint = h('div', { class: 'muted small', style: { position: 'absolute', bottom: '10px', left: '50%', transform: 'translateX(-50%)' }, text: '滚轮缩放 · 拖动平移 · 双击复位' });
  const viewOriginal = h('button', {
    class: 'ghost',
    style: { position: 'absolute', bottom: '44px', left: '50%', transform: 'translateX(-50%)' },
    text: '查看原图',
    onclick: () => showOriginal(),
  });
  setChildren(stage, img, hint, viewOriginal);
  if (!alive()) return;

  /** 换成原图（保留当前缩放状态：只是换 img.src，不动 transform） */
  function showOriginal() {
    if (showingOriginal) return;
    showingOriginal = true;
    viewOriginal.remove();
    img.src = originalSrc;
  }

  /** 切到另一张：回到缩略图，重新给「查看原图」入口 */
  function resetToThumb(src, original) {
    showingOriginal = false;
    originalSrc = original;
    img.src = src;
    if (!viewOriginal.parentNode) stage.append(viewOriginal);
  }

  // 放大即视为要看细节：自动升级成原图（缩略图放大只会糊）
  const zoom = attachZoom(img, () => showOriginal());

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
      resetToThumb(
        thumbPreviewUrl(images[index].id, nt, 'large'),
        rawPreviewUrl(images[index].id, images[index].name, nt),
      );
      zoom.reset();
      update();
      if (onChange) onChange(images[index]);
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
function attachZoom(img, onZoomIn) {
  const state = { scale: 1, x: 0, y: 0, dragging: false, sx: 0, sy: 0, ox: 0, oy: 0 };
  let zoomedIn = false;

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
    // 第一次放大：缩略图放大只会糊，顺势换成原图
    if (clamped > 1 && !zoomedIn && onZoomIn) {
      zoomedIn = true;
      onZoomIn();
    }
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

  // 高码率视频（手机 1080p 常在 15~30Mbps）在弱网 / 组网链路上没法实时播放。
  // 服务端能按需转一份 720p 低码率副本，卡的时候切过去就行。
  if (file.type === 'VIDEO') {
    const hint = h('span', { class: 'muted small' });
    const smooth = h('button', {
      class: 'ghost',
      text: '卡顿？切低码率播放',
      onclick: () => switchToSmooth({ file, el, smooth, hint, alive }),
    });
    // 并行预取整份文件后本机播放：播放阶段完全不碰网络，链路抖动不再引起卡顿
    const cache = h('button', {
      class: 'ghost',
      text: '缓存后播放（弱网更稳）',
      onclick: () => playFromCache({ el, hint, button: cache, alive }),
    });
    const stats = h('div', { class: 'muted small' });
    box.append(h('div', { class: 'media-hint' }, smooth, cache, hint), stats);
    trackPlayback(el, stats, alive);
  }
}

/**
 * 并行拉取整份媒体到本地 Blob，再让 <video> 播它。
 *
 * 与「切低码率」互补：低码率降低每秒需要的字节数，缓存消除播放期间的网络依赖。
 * 缓存的是**当前正在播的地址**（原片或低码率版本），换码率后再点一次即可。
 * 必须等整份拉完才能起播，所以做成用户显式点的按钮，不做默认路径。
 */
async function playFromCache({ el, hint, button, alive }) {
  const source = el.currentSrc || el.src;
  if (!source || source.startsWith('blob:')) {
    hint.textContent = '当前已经在播放本机缓存';
    return;
  }
  button.disabled = true;
  const startedAt = Date.now();
  try {
    hint.textContent = '正在缓存…';
    const blob = await prefetchMedia({
      url: source,
      isAlive: alive,
      onProgress: ({ percent, bytesPerSecond }) => {
        if (!alive()) return;
        hint.textContent = `缓存中 ${percent}% · ${fmtRate(bytesPerSecond)}`;
      },
    });
    if (!alive()) return;
    const at = el.currentTime || 0;
    const wasPlaying = !el.paused;
    const objectUrl = URL.createObjectURL(blob);
    el.src = objectUrl;
    el.load();
    el.addEventListener('loadedmetadata', () => {
      if (at > 0) el.currentTime = at;
      if (wasPlaying) el.play().catch(() => {});
    }, { once: true });
    const seconds = ((Date.now() - startedAt) / 1000).toFixed(1);
    hint.textContent = `已缓存 ${fmtSize(blob.size)}（${seconds}s），从本机播放，不再受网络影响`;
  } catch (error) {
    if (!alive()) return;
    hint.textContent = `缓存失败：${error && error.message ? error.message : error}`;
    button.disabled = false;
  }
}

/** 字节速率 → 人类可读 */
function fmtRate(bytesPerSecond) {
  if (!bytesPerSecond || bytesPerSecond <= 0) return '-';
  const mb = bytesPerSecond / (1024 * 1024);
  if (mb >= 1) return `${mb.toFixed(1)}MB/s`;
  return `${(bytesPerSecond / 1024).toFixed(0)}KB/s`;
}

/**
 * 播放诊断：给出「卡顿次数 / 累计停顿 / 已缓冲」。
 *
 * 排查视频卡顿时，光看服务端日志分不清「网络供不上」还是「客户端解不动」：
 *   - 缓冲一直是 0 秒、停顿次数不断增加 ⇒ 网络/带宽；
 *   - 缓冲有十几秒还停顿 ⇒ 客户端解码或渲染跟不上。
 * 数字直接显示在播放器下面，用户报数就能定位。
 */
function trackPlayback(el, stats, alive) {
  let stalls = 0;
  let stallMs = 0;
  let waitingAt = 0;
  let reported = -1;

  const render = () => {
    if (!alive()) return;
    const ahead = bufferedAhead(el);
    const line = `卡顿 ${stalls} 次（累计 ${(stallMs / 1000).toFixed(1)}s）· 已缓冲 ${ahead.toFixed(1)}s`;
    if (line !== reported) {
      reported = line;
      stats.textContent = line;
    }
  };

  el.addEventListener('waiting', () => {
    if (waitingAt) return;
    waitingAt = Date.now();
  });
  el.addEventListener('playing', () => {
    if (!waitingAt) return;
    stallMs += Date.now() - waitingAt;
    waitingAt = 0;
    stalls += 1;
    render();
  });
  el.addEventListener('timeupdate', render);
  render();
}

/** 当前播放位置之后已缓冲的秒数 */
function bufferedAhead(el) {
  const ranges = el.buffered;
  const at = el.currentTime || 0;
  if (!ranges || typeof ranges.length !== 'number') return 0;
  for (let i = 0; i < ranges.length; i += 1) {
    if (ranges.start(i) <= at && at <= ranges.end(i)) {
      return Math.max(0, ranges.end(i) - at);
    }
  }
  return 0;
}

/**
 * 切到服务端转好的低码率版本。
 *
 * 第一次点会触发转码（202 + 进度），转完（200/206）再换 <video> 的 src；
 * 换源时保留播放位置与播放状态，避免从头开始。
 */
async function switchToSmooth({ file, el, smooth, hint, alive }) {
  const { ticket: t } = await ticket(file.id, PURPOSE_DOWNLOAD);
  const url = transcodeUrl(file.id, t);
  const at = el.currentTime || 0;
  const wasPlaying = !el.paused;

  smooth.disabled = true;
  for (let i = 0; i < 400; i += 1) {
    if (!alive()) return;
    let res;
    try {
      res = await fetch(url, { headers: { Range: 'bytes=0-0' }, cache: 'no-store' });
    } catch {
      hint.textContent = '网络中断，正在重试…';
      await new Promise((r) => setTimeout(r, 3000));
      continue;
    }
    res.body?.cancel?.();
    if (res.status === 200 || res.status === 206) {
      hint.textContent = '';
      smooth.textContent = '正在用低码率版本';
      swapMediaSource(el, url, at, wasPlaying);
      return;
    }
    if (res.status === 202) {
      let body = {};
      try { body = await res.json(); } catch { /* 进度拿不到就只显示"转码中" */ }
      hint.textContent = body.state === 'queued'
        ? '排队等待转码…'
        : `转码中 ${body.progress || 0}%（转好一次，之后秒开）`;
      await new Promise((r) => setTimeout(r, 3000));
      continue;
    }
    hint.textContent = (await readErrorMessage(res)) || `转码失败（HTTP ${res.status}）`;
    smooth.disabled = false;
    return;
  }
  hint.textContent = '转码超时，稍后再试';
  smooth.disabled = false;
}

/** 换 <video> 的源并恢复播放位置 */
function swapMediaSource(el, url, at, wasPlaying) {
  el.addEventListener('loadedmetadata', () => {
    const duration = el.duration || 0;
    if (at > 0 && (!duration || at < duration)) el.currentTime = at;
    if (wasPlaying) el.play?.()?.catch?.(() => {});
  }, { once: true });
  el.src = url;
  el.load?.();
}

/** 读服务端 JSON 错误里的 error 字段 */
async function readErrorMessage(res) {
  try {
    const body = await res.json();
    return body && body.error ? body.error : '';
  } catch {
    return '';
  }
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

  // Telegram 大文件首次访问返回 202「正在缓存」：Viewer（pdf.js / FlyFish）会把
  // 202 的 JSON 当成坏文件直接报错。先探到 200/206 再挂 Viewer，并把等待原因写出来。
  const status = h('div', { class: 'muted small', text: '正在准备…' });
  setChildren(stage, h('div', { class: 'media-center' }, status));
  const ready = await waitReady(url, status, alive, { tries: 60, interval: 1000 });
  if (!alive()) return;
  if (!ready) {
    setChildren(stage, h('div', { class: 'viewer-hint' },
      h('div', { class: 'muted', text: status.textContent || '文件仍在云端准备，稍后重新打开本页即可预览。' }),
      h('div', { style: { marginTop: '14px' } },
        h('button', {
          class: 'primary',
          text: `下载 ${file.name || '文件'}`,
          onclick: () => triggerDownload(buildDownloadUrl(file.id, t)),
        }))));
    return;
  }

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
