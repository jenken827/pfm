// DOM 小工具：元素构造、弹层、toast、剪贴板。
//
// 约定：所有用户可见文本一律用 textContent 写入（h 的 text 属性），不拼 innerHTML，
// 文件名 / 路径 / 备注里出现 < > & 也不会破坏结构。

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

function appendChild(parent, child) {
  if (child == null || child === false) return;
  if (Array.isArray(child)) {
    child.forEach((c) => appendChild(parent, c));
    return;
  }
  if (child instanceof Node) {
    parent.appendChild(child);
    return;
  }
  parent.appendChild(document.createTextNode(String(child)));
}

/**
 * 创建元素。
 * h('div', { class: 'x', text: '内容', onclick: fn }, child, [child2])
 */
export function h(tag, attrs, ...children) {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) {
      if (value === undefined || value === null || value === false) continue;
      if (key === 'class') node.className = value;
      else if (key === 'text') node.textContent = value;
      else if (key === 'dataset') Object.assign(node.dataset, value);
      else if (key === 'style' && typeof value === 'object') Object.assign(node.style, value);
      else if (key.startsWith('on') && typeof value === 'function') {
        node.addEventListener(key.slice(2).toLowerCase(), value);
      } else if (key === 'value') node.value = value;
      else if (key === 'checked' || key === 'selected' || key === 'disabled' || key === 'hidden') {
        node[key] = Boolean(value);
      } else if (key === 'html') {
        // 只用于内置的静态 SVG，绝不拼接用户数据
        node.innerHTML = value;
      } else {
        node.setAttribute(key, value === true ? '' : String(value));
      }
    }
  }
  children.forEach((c) => appendChild(node, c));
  return node;
}

/**
 * 用新内容替换旧内容。与 replaceChildren 的差别：自动跳过 null / false /
 * undefined（原生 replaceChildren 会把 null 变成文本 "null"，页面上真的会显示出来）。
 */
export function setChildren(node, ...children) {
  node.replaceChildren();
  children.forEach((c) => appendChild(node, c));
  return node;
}

export function clear(node) {
  if (node) node.replaceChildren();
  return node;
}

export function spinner() {
  return h('div', { class: 'spinner' });
}

export function emptyState(text, icon = '📂') {
  return h('div', { class: 'empty' }, h('span', { class: 'big', text: icon }), text);
}

export function errorState(text, onRetry) {
  return h('div', { class: 'empty' },
    h('span', { class: 'big', text: '⚠️' }),
    h('div', { class: 'error-text', text }),
    onRetry ? h('div', { style: { marginTop: '10px' } },
      h('button', { class: 'sm', text: '重试', onclick: onRetry })) : null);
}

/** 底部提示条；kind: '' | 'ok' | 'err' */
export function toast(message, kind = '') {
  // 每次都重新找 #toasts，不做模块级缓存：缓存的节点可能已被替换（骨架重建、换文档），
  // 那样提示会写进一个已经不在文档里的容器，用户什么都看不到。
  let host = $('#toasts');
  if (!host) {
    host = h('div', { id: 'toasts' });
    document.body.appendChild(host);
  }
  const node = h('div', { class: `toast ${kind}`.trim(), text: message });
  host.appendChild(node);
  setTimeout(() => {
    node.style.transition = 'opacity .2s';
    node.style.opacity = '0';
    setTimeout(() => node.remove(), 220);
  }, kind === 'err' ? 4200 : 2600);
}

/**
 * 打开弹层。
 * @returns {{root: HTMLElement, close: () => void}}
 */
export function openModal({ title, body, footer, wide = false, onClose } = {}) {
  const overlay = h('div', { class: 'overlay' });
  const modal = h('div', { class: `modal${wide ? ' wide' : ''}` });
  const close = () => {
    overlay.remove();
    document.removeEventListener('keydown', onKey);
    if (onClose) onClose();
  };
  const onKey = (ev) => {
    if (ev.key === 'Escape') close();
  };

  if (title !== undefined) {
    modal.appendChild(h('div', { class: 'modal-head' },
      h('h2', { text: title }),
      h('button', { class: 'ghost icon', text: '✕', title: '关闭', onclick: close })));
  }
  if (body) modal.appendChild(h('div', { class: 'modal-body' }, body));
  if (footer) modal.appendChild(h('div', { class: 'modal-foot' }, footer));

  overlay.appendChild(modal);
  overlay.addEventListener('click', (ev) => {
    if (ev.target === overlay) close();
  });
  document.addEventListener('keydown', onKey);
  $('#overlays').appendChild(overlay);
  return { root: modal, overlay, close };
}

// 当前打开的行内菜单的关闭函数（同时只允许一个）
let closeOpenMenu = () => {};

/**
 * 在 anchor 旁弹出操作菜单（文件行的「⋮」、右键菜单）。
 *
 * 用 position: fixed 贴住 anchor，越界时自动翻到上方 / 左移；点外部、按 Esc、
 * 滚动或窗口尺寸变化都会关闭——菜单不是模态，不锁滚动也不抢焦点。
 * @param {HTMLElement} anchor 触发元素
 * @param {Array<{text: string, danger?: boolean, onClick?: () => void}>} items
 */
export function openMenu(anchor, items) {
  closeOpenMenu();
  const menu = h('div', { class: 'menu', role: 'menu' });
  items.filter(Boolean).forEach((it) => {
    menu.appendChild(h('button', {
      class: it.danger ? 'danger' : '',
      role: 'menuitem',
      text: it.text,
      onclick: (ev) => {
        ev.stopPropagation();
        close();
        if (it.onClick) it.onClick();
      },
    }));
  });

  const close = () => {
    menu.remove();
    document.removeEventListener('click', onDocClick, true);
    document.removeEventListener('keydown', onKey, true);
    window.removeEventListener('resize', close);
    window.removeEventListener('scroll', close, true);
    closeOpenMenu = () => {};
  };
  const onDocClick = (ev) => {
    if (!menu.contains(ev.target)) close();
  };
  const onKey = (ev) => {
    if (ev.key === 'Escape') close();
  };

  document.body.appendChild(menu);

  // 定位：默认贴 anchor 左下，到底部翻到上方，右边越界则左移
  const rect = anchor.getBoundingClientRect();
  const mw = menu.offsetWidth;
  const mh = menu.offsetHeight;
  let left = rect.right - mw;
  let top = rect.bottom + 6;
  if (top + mh > window.innerHeight - 8) top = Math.max(8, rect.top - mh - 6);
  if (left + mw > window.innerWidth - 8) left = window.innerWidth - mw - 8;
  menu.style.left = `${Math.max(8, left)}px`;
  menu.style.top = `${top}px`;

  // 打开的那一次点击不算"点外部"
  setTimeout(() => {
    document.addEventListener('click', onDocClick, true);
    document.addEventListener('keydown', onKey, true);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
  }, 0);
  closeOpenMenu = close;
  return close;
}

/** 二次确认；返回 Promise<boolean> */
export function confirmDialog(text, { title = '请确认', okText = '确定', danger = false } = {}) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (v) => {
      if (done) return;
      done = true;
      handle.close();
      resolve(v);
    };
    const handle = openModal({
      title,
      body: h('div', { text }),
      footer: [
        h('button', { text: '取消', onclick: () => finish(false) }),
        h('button', { class: danger ? 'primary danger' : 'primary', text: okText, onclick: () => finish(true) }),
      ],
      onClose: () => finish(false),
    });
  });
}

/**
 * 单输入框弹层；返回 Promise<string|null>（取消为 null）。
 * 支持校验：validate(value) 返回错误文案则阻止提交。
 */
export function promptDialog({
  title, label, value = '', placeholder = '', okText = '确定',
  type = 'text', hint = '', validate,
} = {}) {
  return new Promise((resolve) => {
    let done = false;
    const input = h('input', { type, value, placeholder });
    const err = h('div', { class: 'error-text small' });
    const finish = (v) => {
      if (done) return;
      done = true;
      handle.close();
      resolve(v);
    };
    const submit = () => {
      const v = input.value.trim();
      const msg = validate ? validate(v) : (v ? '' : '不能为空');
      if (msg) {
        err.textContent = msg;
        input.focus();
        return;
      }
      finish(v);
    };
    input.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') submit();
    });

    const handle = openModal({
      title,
      body: h('form', {
        onsubmit: (ev) => {
          ev.preventDefault();
          submit();
        },
      },
      h('div', { class: 'field' }, h('label', { text: label }), input, hint ? h('div', { class: 'hint', text: hint }) : null),
      err),
      footer: [
        h('button', { text: '取消', onclick: () => finish(null) }),
        h('button', { class: 'primary', text: okText, onclick: submit }),
      ],
      onClose: () => finish(null),
    });
    setTimeout(() => input.focus(), 30);
  });
}

/** 剪贴板：优先 Clipboard API（https / localhost），否则退到 execCommand */
export async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return true;
  }
  const ta = h('textarea', { value: text, readonly: '', style: { position: 'fixed', opacity: '0' } });
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  ta.remove();
  if (!ok) throw new Error('浏览器拒绝写入剪贴板');
  return true;
}

/** 触发浏览器下载（a[download] + 后端 Content-Disposition） */
export function triggerDownload(url) {
  const a = h('a', { href: url, download: '', style: { display: 'none' } });
  document.body.appendChild(a);
  a.click();
  setTimeout(() => a.remove(), 0);
}
