// 最小的假 DOM：只实现 Web UI 实际用到的那部分（见 js/dom.js 与各视图）。
//
// 为什么不用 jsdom：项目是"无构建步骤"的 ES modules（//go:embed 直接打包），
// 引入 npm 依赖会让一条 `go test` 之外再多出一套 node_modules 与锁文件。
// 这里够用的实现约 250 行，配 node:test 就能把视图渲染跑起来。

export class FakeNode {
  constructor() {
    this.parentNode = null;
    this.childNodes = [];
  }

  get parentElement() {
    return this.parentNode instanceof FakeElement ? this.parentNode : null;
  }

  get nextSibling() {
    if (!this.parentNode) return null;
    const list = this.parentNode.childNodes;
    return list[list.indexOf(this) + 1] || null;
  }

  get firstChild() {
    return this.childNodes[0] || null;
  }

  get children() {
    return this.childNodes.filter((n) => n instanceof FakeElement);
  }

  appendChild(node) {
    node.remove?.();
    node.parentNode = this;
    this.childNodes.push(node);
    return node;
  }

  insertBefore(node, ref) {
    const i = this.childNodes.indexOf(ref);
    node.parentNode = this;
    this.childNodes.splice(i < 0 ? this.childNodes.length : i, 0, node);
    return node;
  }

  append(...nodes) {
    nodes.forEach((n) => this.appendChild(typeof n === 'string' ? new FakeText(n) : n));
  }

  prepend(...nodes) {
    nodes.slice().reverse().forEach((n) => {
      const node = typeof n === 'string' ? new FakeText(n) : n;
      this.insertBefore(node, this.firstChild);
    });
  }

  replaceChildren(...nodes) {
    this.childNodes.forEach((n) => { n.parentNode = null; });
    this.childNodes = [];
    nodes.forEach((n) => this.appendChild(n));
  }

  remove() {
    if (!this.parentNode) return;
    const list = this.parentNode.childNodes;
    const i = list.indexOf(this);
    if (i >= 0) list.splice(i, 1);
    this.parentNode = null;
  }

  contains(node) {
    for (let n = node; n; n = n.parentNode) {
      if (n === this) return true;
    }
    return false;
  }
}

export class FakeText extends FakeNode {
  constructor(text) {
    super();
    this.nodeType = 3;
    this.data = String(text);
  }

  get textContent() { return this.data; }
  set textContent(v) { this.data = String(v); }
}

export class FakeElement extends FakeNode {
  constructor(tag) {
    super();
    this.nodeType = 1;
    this.tagName = String(tag).toUpperCase();
    this.attributes = new Map();
    this.dataset = {};
    this.style = {};
    this._class = '';
    this._listeners = new Map();
    this._id = '';
    this.value = '';
    this.checked = false;
    this.selected = false;
    this.disabled = false;
    this.hidden = false;
    this.scrollTop = 0;
    this.scrollHeight = 0;
    this.clientHeight = 0;
    this.offsetWidth = 0;
    this.offsetHeight = 0;
  }

  get id() { return this._id || this.attributes.get('id') || ''; }
  set id(v) { this._id = String(v); }

  get className() { return this._class; }
  set className(v) { this._class = String(v || ''); }

  get classList() {
    const self = this;
    const parts = () => self._class.split(/\s+/).filter(Boolean);
    return {
      add: (...names) => { self._class = [...new Set([...parts(), ...names])].join(' '); },
      remove: (...names) => { self._class = parts().filter((c) => !names.includes(c)).join(' '); },
      contains: (name) => parts().includes(name),
      toggle: (name, force) => {
        const has = parts().includes(name);
        const want = force === undefined ? !has : Boolean(force);
        if (want && !has) self._class = [...parts(), name].join(' ');
        if (!want && has) self._class = parts().filter((c) => c !== name).join(' ');
        return want;
      },
      get length() { return parts().length; },
    };
  }

  get textContent() {
    return this.childNodes.map((n) => n.textContent).join('');
  }

  set textContent(v) {
    this.childNodes.forEach((n) => { n.parentNode = null; });
    this.childNodes = [];
    // 浏览器把 null / undefined 变成空串，视图里确实会传 undefined
    const text = v === null || v === undefined ? '' : String(v);
    if (text !== '') this.appendChild(new FakeText(text));
  }

  // 只有 dom.js 给内置静态 SVG 用；当作纯文本存起来，够断言用
  set innerHTML(v) { this.textContent = String(v || '').replace(/<[^>]*>/g, ''); }
  get innerHTML() { return this.textContent; }

  setAttribute(name, value) { this.attributes.set(name, String(value)); }

  getAttribute(name) {
    if (name === 'id') return this.id || null;
    if (name === 'class') return this._class || null;
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }

  removeAttribute(name) {
    this.attributes.delete(name);
    if (name === 'id') this._id = '';
    if (name === 'class') this._class = '';
  }

  addEventListener(type, fn) {
    if (!this._listeners.has(type)) this._listeners.set(type, []);
    this._listeners.get(type).push(fn);
  }

  removeEventListener(type, fn) {
    const list = this._listeners.get(type);
    if (!list) return;
    this._listeners.set(type, list.filter((f) => f !== fn));
  }

  dispatchEvent(ev) {
    ev.target = ev.target || this;
    ev.currentTarget = this;
    let node = this;
    while (node) {
      (node._listeners?.get(ev.type) || []).forEach((fn) => fn.call(node, ev));
      node = node.parentNode;
    }
    return true;
  }

  click() {
    this.dispatchEvent(new FakeEvent('click'));
  }

  // 媒体元素的最小 API：视图里 <video> 会调 load/play/pause
  load() {}
  play() { this.paused = false; return Promise.resolve(); }
  pause() { this.paused = true; }

  focus() { FakeElement.activeElement = this; }
  blur() { if (FakeElement.activeElement === this) FakeElement.activeElement = null; }
  select() {}
  scrollIntoView() {}

  getBoundingClientRect() {
    return { top: 0, left: 0, right: 200, bottom: 20, width: 200, height: 20 };
  }

  closest(selector) {
    for (let node = this; node; node = node.parentNode) {
      if (node instanceof FakeElement && node.matches(selector)) return node;
    }
    return null;
  }

  matches(selector) {
    return selector.split(',').some((sel) => matchSimple(this, sel.trim()));
  }

  querySelectorAll(selector) {
    const out = [];
    const walk = (node) => {
      node.childNodes.forEach((child) => {
        if (child instanceof FakeElement) {
          if (child.matches(selector)) out.push(child);
          walk(child);
        }
      });
    };
    walk(this);
    return out;
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] || null;
  }
}

FakeElement.activeElement = null;

/** 复合选择器：tag / #id / .class / tag.a.b / [attr] / [attr=value]，可带后代组合 */
function matchSimple(node, selector) {
  const parts = selector.split(/\s+/).filter(Boolean);
  if (parts.length > 1) {
    // 后代选择器：自身匹配最后一段，且祖先里能命中前面的部分
    if (!matchCompound(node, parts[parts.length - 1])) return false;
    let rest = parts.slice(0, -1);
    let cur = node.parentNode;
    while (cur && rest.length) {
      if (cur instanceof FakeElement && matchCompound(cur, rest[rest.length - 1])) rest = rest.slice(0, -1);
      cur = cur.parentNode;
    }
    return rest.length === 0;
  }
  return matchCompound(node, selector);
}

function matchCompound(node, selector) {
  const tokens = selector.match(/^([a-zA-Z0-9*-]+)?((?:[.#][^.#[\]]+|\[[^\]]+\])*)$/);
  if (!tokens) return false;
  const [, tag, rest] = tokens;
  if (tag && tag !== '*' && node.tagName !== tag.toUpperCase()) return false;
  const parts = rest.match(/[.#][^.#[\]]+|\[[^\]]+\]/g) || [];
  for (const part of parts) {
    if (part.startsWith('#')) {
      if (node.id !== part.slice(1)) return false;
    } else if (part.startsWith('.')) {
      if (!node.classList.contains(part.slice(1))) return false;
    } else {
      const m = part.slice(1, -1).match(/^([^=]+)(?:="?([^"]*)"?)?$/);
      if (!m) return false;
      const [, name, want] = m;
      const got = node.getAttribute(name);
      if (got === null) return false;
      if (want !== undefined && got !== want) return false;
    }
  }
  return true;
}

export class FakeEvent {
  constructor(type, init = {}) {
    this.type = type;
    this.target = null;
    this.currentTarget = null;
    this.defaultPrevented = false;
    Object.assign(this, init);
  }

  preventDefault() { this.defaultPrevented = true; }
  stopPropagation() {}
}

export class FakeDocument extends FakeNode {
  constructor() {
    super();
    this.nodeType = 9;
    this.documentElement = new FakeElement('html');
    this.head = new FakeElement('head');
    this.body = new FakeElement('body');
    this.documentElement.appendChild(this.head);
    this.documentElement.appendChild(this.body);
    this.appendChild(this.documentElement);
    this._listeners = new Map();
    this.execCommand = () => true;
  }

  createElement(tag) { return new FakeElement(tag); }
  createTextNode(text) { return new FakeText(text); }

  get activeElement() { return FakeElement.activeElement; }

  addEventListener(type, fn) {
    if (!this._listeners.has(type)) this._listeners.set(type, []);
    this._listeners.get(type).push(fn);
  }

  removeEventListener(type, fn) {
    this._listeners.set(type, (this._listeners.get(type) || []).filter((f) => f !== fn));
  }

  dispatchEvent(ev) {
    ev.target = ev.target || this;
    (this._listeners.get(ev.type) || []).forEach((fn) => fn(ev));
    return true;
  }

  querySelectorAll(selector) {
    const out = [];
    const walk = (node) => {
      node.childNodes.forEach((child) => {
        if (child instanceof FakeElement) {
          if (child.matches(selector)) out.push(child);
          walk(child);
        }
      });
    };
    walk(this);
    return out;
  }

  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
}
