// 外壳（app.js）测试：登录闸门、管理员导航显隐、退出后再登录。
//
// app.js 在导入时就 boot()，所以整个文件共用一个外壳实例，用例按顺序推进状态
// （登录 → 看导航 → 退出 → 换普通用户登录）。状态相关的东西都从 localStorage 走，
// 这样和真实浏览器行为一致。

import test from 'node:test';
import assert from 'node:assert/strict';

import { install, RAW } from './support/harness.mjs';

const ROUTES = {
  '/api/auth/login': (req) => {
    const { username } = req.body || {};
    if (username === 'smoke') return RAW({ token: 'jwt-admin', name: 'smoke', isAdmin: true, userId: 'u-1' });
    if (username === 'li_test') return RAW({ token: 'jwt-user', name: 'li_test', isAdmin: false, userId: 'u-2' });
    return { $error: { status: 401, body: { message: 'bad credentials' } } };
  },
  '/api/space/get': { space: { rootFileId: 'root-1', usedBytes: '0', quotaBytes: '0', boundProviderIds: '[1]' } },
  '/api/file/list': { files: [], total: '0', hasMore: false },
};

const env = install(ROUTES, { token: null, user: null });
const { app } = await import('../js/app.js');
await env.settle();

const navGroups = () => env.document.querySelectorAll('.nav-group').map((g) => ({
  title: g.querySelector('.nav-title')?.textContent || '',
  hidden: Boolean(g.hidden),
}));

async function loginAs(username) {
  const inputs = env.document.querySelectorAll('.login-wrap input');
  inputs[0].value = username;
  inputs[1].value = 'whatever';
  env.document.querySelector('.login-wrap form').dispatchEvent(new (await import('./support/fake_dom.mjs')).FakeEvent('submit'));
  await env.settle();
}

test('未登录时只显示登录页', async () => {
  assert.match(env.text(), /用服务端账号登录/);
  assert.equal(app.sidebar.parentElement.hidden, true, '没登录不该看到侧边栏');
});

test('管理员登录后出现「管理」组', async () => {
  await loginAs('smoke');
  const groups = navGroups();
  const admin = groups.find((g) => g.title === '管理');
  assert.ok(admin, '应当有管理组');
  assert.equal(admin.hidden, false);
  assert.ok(env.calls.includes('/api/file/list'), '登录后进入文件页');
});

test('退出后回到登录页，再以普通用户登录时「管理」组隐藏', async () => {
  env.clickButton('退出');
  await env.settle();
  assert.match(env.text(), /用服务端账号登录/);

  await loginAs('li_test');
  const admin = navGroups().find((g) => g.title === '管理');
  assert.ok(admin, '管理组仍在 DOM 里（只是隐藏）');
  assert.equal(admin.hidden, true, '非管理员不该看到管理入口');
  assert.match(env.text(), /我的文件/, '普通用户照样能用文件页');
});

test('普通用户直接敲 #/admin/users 只会看到锁定提示', async () => {
  env.window.location.hash = '#/admin/users';
  await env.settle();
  assert.match(env.text(), /只有管理员能看/);
});
