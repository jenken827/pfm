// 登录页：服务端本地账号（与 App 相同），登录后 JWT 只放内存 + localStorage。

import { h, setChildren } from '../dom.js';
import { login } from '../session.js';

/**
 * 渲染登录卡片。
 * @param {HTMLElement} root 容器
 * @param {() => void} onSuccess 登录成功回调
 */
export function renderLogin(root, onSuccess) {
  const err = h('div', { class: 'error-text small' });
  const username = h('input', { type: 'text', placeholder: '用户名', autocomplete: 'username', required: '' });
  const password = h('input', { type: 'password', placeholder: '密码', autocomplete: 'current-password', required: '' });
  const submit = h('button', { class: 'primary', type: 'submit', text: '登录' });

  const form = h('form', {
    onsubmit: async (ev) => {
      ev.preventDefault();
      err.textContent = '';
      submit.disabled = true;
      submit.textContent = '登录中…';
      try {
        await login(username.value.trim(), password.value);
        password.value = '';
        onSuccess();
      } catch (e) {
        err.textContent = e.message;
      } finally {
        submit.disabled = false;
        submit.textContent = '登录';
      }
    },
  },
  h('div', { class: 'field' }, username),
  h('div', { class: 'field' }, password),
  submit);

  setChildren(root, h('div', { class: 'card login-card' },
    h('div', { class: 'logo-row' },
      h('div', { class: 'logo', text: '📁' }),
      h('div', null,
        h('h1', { text: 'PFM 文件浏览' }),
        h('div', { class: 'muted small', text: '用服务端账号登录' }))),
    form,
    err,
    h('p', { class: 'muted small', text: '浏览器版提供预览与常用文件管理；上传、备份、相册请在客户端操作。' })));

  setTimeout(() => username.focus(), 30);
}
