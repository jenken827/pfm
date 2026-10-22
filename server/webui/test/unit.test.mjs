// 纯逻辑单元测试：格式化、路由解析、接口错误映射、URL 构造、预览分流、文本白名单。
//
// 这些模块是"两端必须一致"的判定中心（预览分流、文本白名单、票据 URL），
// 出错的表现往往很隐蔽（某类文件打不开、URL 里混进了 access token），
// 所以这里逐条钉住。

import test from 'node:test';
import assert from 'node:assert/strict';

import { install, RAW } from './support/harness.mjs';

const env = install({});
void env;

const fmt = await import('../js/format.js');
const router = await import('../js/router.js');
const api = await import('../js/api.js');
const session = await import('../js/session.js');
const { previewKindOf } = await import('../js/views/preview_kind.js');
const text = await import('../js/text_preview.js');

// ---------------------------------------------------------------- format

test('fmtSize：单位进位与非法值', () => {
  assert.equal(fmt.fmtSize(512), '512 B');
  assert.equal(fmt.fmtSize(1023), '1023 B');
  assert.equal(fmt.fmtSize(2048), '2.0 KB');
  assert.equal(fmt.fmtSize(5 * 1024 ** 3), '5.0 GB');
  assert.equal(fmt.fmtSize(0), '-', '文件大小列里 0 / 未知都显示 -');
  assert.equal(fmt.fmtSize(null), '-');
  assert.equal(fmt.fmtSize('abc'), '-');
});

test('fmtSize0：用量列里 0 要显示成 0 B', () => {
  assert.equal(fmt.fmtSize0(0), '0 B');
  assert.equal(fmt.fmtSize0(null), '0 B');
  assert.equal(fmt.fmtSize0(undefined), '0 B');
  assert.equal(fmt.fmtSize0(2048), '2.0 KB');
});

test('fmtTime / relTime / fmtRemain：空值与边界', () => {
  assert.equal(fmt.fmtTime(''), '-');
  assert.equal(fmt.fmtTime(undefined), '-');
  assert.match(fmt.fmtTime('2026-10-20T10:05:00Z'), /^2026-10-2\d \d{2}:\d{2}$/);
  assert.equal(fmt.relTime(''), '-');
  assert.equal(fmt.relTime(new Date().toISOString()), '刚刚');
  assert.equal(fmt.fmtRemain(''), '永不过期');
  assert.equal(fmt.fmtRemain('2000-01-01T00:00:00Z'), '已过期');
  assert.match(fmt.fmtRemain('2099-01-01T00:00:00Z'), /天后过期$/);
});

test('状态枚举 → 中文（上传任务 / 分享 / 落地位置）', () => {
  assert.deepEqual(fmt.uploadState('FAILED'), ['失败', 'bad']);
  assert.deepEqual(fmt.uploadState('COMPLETED'), ['已完成', 'ok']);
  assert.deepEqual(fmt.uploadState('PAUSED'), ['已暂停', 'warn']);
  assert.deepEqual(fmt.uploadState('什么也不是'), ['未知', '']);
  assert.deepEqual(fmt.shareStatus('ACTIVE'), ['有效', 'ok']);
  assert.deepEqual(fmt.locStatus('PCOMPLETED'), ['已完成', 'ok']);
  assert.deepEqual(fmt.downloadState('DOWNLOAD_FAILED'), ['失败', 'bad']);
  assert.equal(fmt.taskType('BACKUP'), '备份');
  assert.equal(fmt.taskType('SYNC'), '同步');
  assert.equal(fmt.taskType('UPLOAD'), '上传');
  assert.equal(fmt.taskType(''), '上传');
  assert.equal(fmt.typeText('DOCUMENT'), '文档');
  assert.equal(fmt.typeText('WHATEVER'), '其他');
});

test('sortFiles：文件夹恒在前、数字感知排序、倒序不把文件夹甩到后面', () => {
  const files = [
    { name: 'b.txt', type: 'DOCUMENT', size: 1 },
    { name: 'a2.txt', type: 'DOCUMENT', size: 3 },
    { name: 'a10.txt', type: 'DOCUMENT', size: 2 },
    { name: 'zdir', type: 'FOLDER' },
    { name: 'adir', type: 'FOLDER' },
  ];
  const asc = fmt.sortFiles(files, 'name', 1).map((f) => f.name);
  assert.deepEqual(asc, ['adir', 'zdir', 'a2.txt', 'a10.txt', 'b.txt']);
  const desc = fmt.sortFiles(files, 'name', -1).map((f) => f.name);
  assert.deepEqual(desc.slice(0, 2), ['adir', 'zdir'], '倒序时文件夹仍在最前');
  assert.deepEqual(fmt.sortFiles(files, 'size', 1).filter((f) => f.type !== 'FOLDER').map((f) => f.name),
    ['b.txt', 'a10.txt', 'a2.txt']);
});

test('shortHash：短串原样、长串截断', () => {
  assert.equal(fmt.shortHash('abc'), 'abc');
  assert.equal(fmt.shortHash(''), '-');
  assert.equal(fmt.shortHash('0123456789abcdefgh'), '0123456789abcdef…');
});

// ---------------------------------------------------------------- router

test('router.parse：每个页面都能解析出正确的 name 与参数', () => {
  const cases = [
    ['#/files', 'files'],
    ['#/files/abc-1', 'files'],
    ['#/recent', 'recent'],
    ['#/type/IMAGE', 'type'],
    ['#/preview/abc-1', 'preview'],
    ['#/detail/abc-1', 'detail'],
    ['#/settings/abc-1', 'settings'],
    ['#/trash', 'trash'],
    ['#/shares', 'shares'],
    ['#/offline', 'offline'],
    ['#/transfers', 'transfers'],
    ['#/webdav', 'webdav'],
    ['#/admin/users', 'adminUsers'],
    ['#/admin/providers', 'adminProviders'],
    ['#/admin/backup', 'adminBackup'],
    ['#/login', 'login'],
  ];
  for (const [hash, name] of cases) {
    assert.equal(router.parse(hash).name, name, `${hash} 应解析成 ${name}`);
  }
  assert.equal(router.parse('#/files/abc-1').params.id, 'abc-1');
  assert.equal(router.parse('#/settings/dir-1').params.id, 'dir-1');
  assert.equal(router.parse('#/type/DOCUMENT').params.type, 'DOCUMENT');
  assert.equal(router.parse('#/preview/x?from=dir-9').query.get('from'), 'dir-9');
});

test('router.parse：认不出的 hash 回落到文件页（不让用户卡在白页）', () => {
  assert.equal(router.parse('#/no-such-page').name, 'files');
  assert.equal(router.parse('#/').name, 'files');
  assert.equal(router.parse('').name, 'files');
});

test('router.href：拼 query 且不重复加 ?', () => {
  assert.equal(router.href('/files'), '#/files');
  assert.equal(router.href('/search', { q: '报告' }), '#/search?q=%E6%8A%A5%E5%91%8A');
});

// ---------------------------------------------------------------- api

test('api.call：成功时返回 data，不带 @type 依赖', async () => {
  install({ '/api/file/list': { files: [{ id: 'a' }], total: '1' } });
  const data = await api.call('/api/file/list', { parentId: 'root-1' });
  assert.deepEqual(data, { files: [{ id: 'a' }], total: '1' });
});

test('api.call：业务错误码 → 中文短语，同时保留服务端原文', async () => {
  install({
    '/api/admin/backup/task/create': {
      $error: { code: 'PARAMS_ERROR', message: 'source path does not exist' },
    },
  });
  await assert.rejects(
    () => api.call('/api/admin/backup/task/create', { sourcePath: '/nope' }),
    (err) => {
      assert.equal(err.name, 'ApiError');
      assert.equal(err.code, 'PARAMS_ERROR');
      assert.equal(err.message, '参数不正确');
      assert.equal(err.rawMessage, 'source path does not exist');
      return true;
    },
  );
});

test('api.call：未知错误码退回服务端原文，HTTP 4xx 也带消息', async () => {
  install({ '/api/x': { $error: { code: 'SOMETHING_NEW', message: '服务端新错误' } } });
  await assert.rejects(() => api.call('/api/x'), /服务端新错误/);

  install({ '/api/y': { $error: { status: 400, body: { code: 'BAD', message: 'bad request' } } } });
  await assert.rejects(
    () => api.call('/api/y'),
    (err) => err.message === 'bad request' && err.rawMessage === 'bad request',
  );
});

test('api.call：401 清会话并要求重新登录', async () => {
  install({ '/api/z': { $error: { status: 401, body: { code: 'TOKEN_INVALID', message: 'expired' } } } });
  session.session.save('jwt', { name: 'x' });
  await assert.rejects(
    () => api.call('/api/z'),
    (err) => err.code === 'TOKEN_INVALID' && /登录已过期/.test(err.message),
  );
  assert.equal(session.session.token, '', '401 之后必须清掉本地 token');
});

test('api.call：网络失败给出可读提示', async () => {
  install({});
  globalThis.fetch = async () => { throw new Error('ECONNREFUSED'); };
  await assert.rejects(() => api.call('/api/anything'), /网络请求失败/);
});

test('URL 里只放票据，绝不放 access token', () => {
  const dl = api.buildDownloadUrl('file-1', 'v1.payload.sig', 2);
  assert.match(dl, /ticket=v1\.payload\.sig/);
  assert.match(dl, /provider_id=2/);
  assert.doesNotMatch(dl, /token=/);

  const direct = api.directUrl('file-1', 'v1.payload.sig');
  assert.match(direct, /^\/api\/file\/stream\/direct\?/);
  assert.doesNotMatch(direct, /access_token|token=/);

  const raw = api.rawPreviewUrl('file-1', '目录/报告 2026.pdf', 'v1.payload.sig');
  assert.match(raw, /^\/api\/file\/preview\/raw\/%E7%9B%AE%E5%BD%95%2F/);
  assert.match(raw, /ticket=/);
  assert.doesNotMatch(raw, /token=/);

  const thumb = api.previewPath('file-1', { size: '200', providerId: 3 });
  assert.match(thumb, /size=200/);
  assert.match(thumb, /provider_id=3/);
});

test('api.ticket：服务端没给票据时明确报错', async () => {
  install({ '/api/file/preview-ticket': {} });
  await assert.rejects(() => api.ticket('file-1'), /服务端未返回票据/);

  install({ '/api/file/preview-ticket': { ticket: 'v1.a.b', expiresAt: '2026-10-20T10:00:00Z' } });
  const t = await api.ticket('file-1');
  assert.equal(t.ticket, 'v1.a.b');
});

// ---------------------------------------------------------------- 预览分流

test('previewKindOf：与客户端 preview_router 同口径', () => {
  assert.equal(previewKindOf({ type: 'IMAGE', name: 'a.jpg' }), 'image');
  assert.equal(previewKindOf({ type: 'VIDEO', name: 'a.mp4' }), 'media');
  assert.equal(previewKindOf({ type: 'AUDIO', name: 'a.mp3' }), 'media');
  assert.equal(previewKindOf({ type: 'DOCUMENT', name: 'a.pdf' }), 'document');
  assert.equal(previewKindOf({ type: 'ARCHIVE', name: 'a.zip' }), 'document');
  assert.equal(previewKindOf({ type: 'DOCUMENT', name: 'a.md' }), 'text');
  assert.equal(previewKindOf({ type: 'APPLICATION', name: 'Dockerfile' }), 'text');
  assert.equal(previewKindOf({ type: 'FILE_TYPE_UNKNOWN', name: 'noext' }), 'detail');
  assert.equal(previewKindOf({ type: 'FOLDER', name: 'dir' }), 'detail');
  assert.equal(previewKindOf(null), 'detail');
});

test('text_preview：扩展名 / 已知文件名 / 点文件', () => {
  assert.equal(text.isTextFileName('a.md'), true);
  assert.equal(text.isTextFileName('报告.JSON'), true, '大小写不敏感');
  assert.equal(text.isTextFileName('README'), true, '无扩展名的已知文件名');
  assert.equal(text.isTextFileName('.gitignore'), true);
  assert.equal(text.isTextFileName('a.bin'), false);
  assert.equal(text.isTextFileName('dir.d/a'), false);
  assert.equal(text.isMarkdownFile('a.MD'), true);
  assert.equal(text.isMarkdownFile('a.txt'), false);
});

test('text_preview：图片 / 视频不会被当文本打开', () => {
  assert.equal(text.isTextPreviewable({ type: 'IMAGE', name: 'a.md' }), false);
  assert.equal(text.isTextPreviewable({ type: 'DOCUMENT', name: 'a.md' }), true);
  assert.equal(text.isTextPreviewable({ type: 'FILE_TYPE_UNKNOWN', name: 'a.conf' }), true);
  assert.equal(text.isTextPreviewable({ type: 'FOLDER', name: 'a' }), false);
});

test('renderMarkdown：转义 HTML，不执行用户内容', () => {
  const html = text.renderMarkdown('# 标题\n\n<script>alert(1)</script>\n\n**粗体**');
  assert.match(html, /<h1>标题<\/h1>/);
  assert.match(html, /<strong>粗体<\/strong>/);
  assert.doesNotMatch(html, /<script>/);
});

test('decodeTextBytes：UTF-8 与 BOM', () => {
  const utf8 = new TextEncoder().encode('中文内容');
  assert.equal(text.decodeTextBytes(utf8).content, '中文内容');
  const bom = new Uint8Array([0xef, 0xbb, 0xbf, 0x41]);
  assert.equal(text.decodeTextBytes(bom).content, 'A');
});

// ---------------------------------------------------------------- 会话

test('session：登录成功写入 token 与用户信息，expire 清空', async () => {
  const e = install({ '/api/auth/login': RAW({ token: 'jwt-1', name: 'smoke', isAdmin: true, userId: 'u-1' }) });
  const user = await session.login('smoke', 'smoke123');
  assert.equal(user.isAdmin, true);
  assert.equal(session.session.token, 'jwt-1');
  assert.equal(e.document ? globalThis.localStorage.getItem('pfm_web_token') : '', 'jwt-1');

  session.expireSession();
  assert.equal(session.session.token, '');
  assert.equal(globalThis.localStorage.getItem('pfm_web_token'), null);
});

test('session：401 被翻译成"用户名或密码错误"', async () => {
  install({ '/api/auth/login': { $error: { status: 401, body: {} } } });
  await assert.rejects(() => session.login('a', 'b'), /用户名或密码错误/);
});
