// 主题：跟随系统 / 强制浅色 / 强制深色 三态循环，选择存在 localStorage。
//
// 实际配色在 css/app.css 里：:root 是浅色，媒体查询跟随系统深色，
// <html data-theme="light|dark"> 用来手动覆盖（=不写 data-theme 就是跟随系统）。

const KEY = 'pfm_web_theme';
const MODES = ['system', 'light', 'dark'];
const LABELS = { system: '跟随系统', light: '浅色', dark: '深色' };
const ICONS = { system: '🌗', light: '☀️', dark: '🌙' };

export function themeMode() {
  let v = '';
  try {
    v = localStorage.getItem(KEY) || '';
  } catch {
    v = '';
  }
  return MODES.includes(v) ? v : 'system';
}

/** 应用主题并持久化；mode = system | light | dark */
export function applyTheme(mode) {
  const m = MODES.includes(mode) ? mode : 'system';
  if (m === 'system') document.documentElement.removeAttribute('data-theme');
  else document.documentElement.setAttribute('data-theme', m);
  try {
    if (m === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, m);
  } catch {
    /* 存不了也能用，只是下次回到跟随系统 */
  }
  return m;
}

/** 循环切换：跟随系统 → 浅色 → 深色 → 跟随系统 */
export function cycleTheme() {
  const next = MODES[(MODES.indexOf(themeMode()) + 1) % MODES.length];
  return applyTheme(next);
}

export function themeLabel(mode = themeMode()) {
  return LABELS[mode] || LABELS.system;
}

export function themeIcon(mode = themeMode()) {
  return ICONS[mode] || ICONS.system;
}
