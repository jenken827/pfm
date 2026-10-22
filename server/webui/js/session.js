// 会话状态：登录态只存 JWT 与用户展示信息，不含任何文件凭证。
//
// token 放 localStorage 是"浏览器端唯一可用的持久化"：页面刷新后仍免登录。
// 它的暴露面与 App 一致（同源脚本可读），因此 **绝不把它拼进任何 URL**——
// 预览 / 下载走单文件票据（见 api.js 的 ticket()）。

const TOKEN_KEY = 'pfm_web_token';
const USER_KEY = 'pfm_web_user';

export const session = {
  token: '',
  user: null,

  load() {
    this.token = localStorage.getItem(TOKEN_KEY) || '';
    try {
      this.user = JSON.parse(localStorage.getItem(USER_KEY) || 'null');
    } catch {
      this.user = null;
    }
    return this;
  },

  save(token, user) {
    this.token = token;
    this.user = user || null;
    localStorage.setItem(TOKEN_KEY, token);
    if (user) localStorage.setItem(USER_KEY, JSON.stringify(user));
  },

  clear() {
    this.token = '';
    this.user = null;
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
  },

  get isLoggedIn() {
    return Boolean(this.token);
  },
};

/** 登录失效：清态并通知 app 回到登录页（由 app.js 监听） */
export function expireSession() {
  session.clear();
  window.dispatchEvent(new Event('pfm:unauthorized'));
}

/** 登录；成功后写入会话，返回用户信息 */
export async function login(username, password) {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (res.status === 401) throw new Error('用户名或密码错误');
  if (!res.ok) throw new Error(`登录失败（HTTP ${res.status}）`);
  const data = await res.json();
  if (!data.token) throw new Error('登录响应缺少 token');
  const user = {
    userId: data.userId || '',
    name: data.name || username,
    email: data.email || '',
    isAdmin: Boolean(data.isAdmin),
    avatar: data.avatar || '',
  };
  session.save(data.token, user);
  return user;
}
