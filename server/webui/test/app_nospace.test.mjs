// 没有分配根目录的账号：外壳要给出"请联系管理员"的提示，而不是统一的"参数不正确"。
// 单独一个文件是因为 store 会缓存用户空间，和 app.test.mjs 共用进程会互相污染。

import test from 'node:test';
import assert from 'node:assert/strict';

import { install, RAW } from './support/harness.mjs';

const env = install({
  '/api/auth/login': RAW({ token: 'jwt-1', name: 'fresh', isAdmin: false, userId: 'u-9' }),
  '/api/space/get': { $error: { code: 'PARAMS_ERROR', message: 'user space not found, please login again to auto-create' } },
}, { token: null, user: null });

await import('../js/app.js');
await env.settle();

test('没有空间时提示联系管理员，而不是"参数不正确"', async () => {
  const inputs = env.document.querySelectorAll('.login-wrap input');
  inputs[0].value = 'fresh';
  inputs[1].value = 'x';
  env.document.querySelector('.login-wrap form').dispatchEvent(new (await import('./support/fake_dom.mjs')).FakeEvent('submit'));
  await env.settle();
  const text = env.text();
  assert.match(text, /还没有分配存储空间/);
  assert.match(text, /用户与配额/);
  assert.doesNotMatch(text, /参数不正确/);
});
