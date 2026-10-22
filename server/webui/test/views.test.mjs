// Web UI 的渲染测试：用假 DOM + 假接口把每个视图跑一遍。
//
// 目的不是像素级校验，而是拦住"点开某一页就白屏"这一类问题：模块导入写错、
// 用了没定义的变量、把 undefined 直接渲染出来、接口字段改名后视图没跟上。
// 断言只用"用户看得见的文案"，尽量不绑 class / 结构，改样式不会误报。

import test from 'node:test';
import assert from 'node:assert/strict';

import { install, renderView, mountShell, FAIL, RAW, HTTP } from './support/harness.mjs';

const ts = '2026-10-20T10:00:00Z';

// js/ 里的模块在"导入时"就会用到浏览器全局（store.js 读 localStorage 的视图偏好），
// 而 ESM 的静态 import 会先于用例执行，所以这里先装一套默认环境，再把视图一次性导入。
// 每个用例自己再 install(<接口桩>) 换环境，已导入的模块会复用（模块级缓存不重复求值）。
install();

const V = {
  files: await import('../js/views/files.js'),
  trash: await import('../js/views/trash.js'),
  shares: await import('../js/views/shares.js'),
  offline: await import('../js/views/offline.js'),
  transfers: await import('../js/views/transfers.js'),
  webdav: await import('../js/views/webdav.js'),
  adminUsers: await import('../js/views/admin_users.js'),
  adminProviders: await import('../js/views/admin_providers.js'),
  adminBackup: await import('../js/views/admin_backup.js'),
  settings: await import('../js/views/settings.js'),
  detail: await import('../js/views/detail.js'),
  login: await import('../js/views/login.js'),
  fakeDom: await import('./support/fake_dom.mjs'),
};

const FILE = (over = {}) => ({
  id: 'file-1', name: '报告.pdf', type: 'DOCUMENT', parentId: 'root-1',
  size: '2048', hash: 'abcdef1234567890', mime: 'application/pdf',
  isReady: true, updatedAt: ts, createdAt: ts, ...over,
});

const FOLDER = (over = {}) => ({
  id: 'dir-1', name: '文档', type: 'FOLDER', parentId: 'root-1',
  size: '0', hash: '', isReady: true, updatedAt: ts, createdAt: ts,
  boundProviderIds: '[1]', enableEncryption: false, maxHistories: 0, ...over,
});

// ---------------------------------------------------------------- files

const LIST = {
  '/api/file/list': { files: [FOLDER(), FILE()], total: '2', hasMore: false },
  '/api/file/search': { files: [FILE()], total: '1', hasMore: false },
  '/api/file/recent': { files: [FILE()], total: '1', hasMore: false },
  '/api/file/by-type': { files: [FILE()], total: '1', hasMore: false },
};

test('files：目录列表 / 空目录 / 接口报错', async () => {

  let env = install(LIST);
  await renderView(env, V.files.renderFiles, { params: {} });
  assert.match(env.text(), /共 2 项/);
  assert.match(env.text(), /文档/);
  assert.match(env.text(), /报告\.pdf/);
  assert.ok(env.calls.includes('/api/file/list'), '应当按根目录拉列表');

  env = install({ ...LIST, '/api/file/list': { files: [], total: '0', hasMore: false } });
  await renderView(env, V.files.renderFiles, { params: {} });
  assert.match(env.text(), /这里还没有文件/);

  env = install({ ...LIST, '/api/file/list': FAIL('PARAMS_ERROR', 'folder not found') });
  await renderView(env, V.files.renderFiles, { params: {} });
  assert.match(env.text(), /参数不正确/);
  assert.match(env.text(), /重试/);
});

test('files：搜索模式走 search 接口并带上关键词', async () => {
  const env = install(LIST);
  await renderView(env, V.files.renderFiles, { name: 'search', params: {}, query: new URLSearchParams('q=报告'), path: '/search' });
  assert.ok(env.calls.includes('/api/file/search'));
  const req = env.requests.find((r) => r.path === '/api/file/search');
  assert.equal(req.body.keyword, '报告');
  assert.equal(req.body.searchType, 'NAME');
});

test('files：按类型浏览把枚举透传给 by-type', async () => {
  const env = install(LIST);
  await renderView(env, V.files.renderFiles, { name: 'type', params: { type: 'DOCUMENT' }, path: '/type' });
  const req = env.requests.find((r) => r.path === '/api/file/by-type');
  assert.equal(req.body.fileType, 'DOCUMENT');
});

// ---------------------------------------------------------------- trash

const TRASH = {
  '/api/file/trash/list': {
    files: [FILE({ id: 'trash-1', name: '旧文件.zip', type: 'ARCHIVE' })],
    total: '1',
  },
};

test('trash：回收站列表 / 空态', async () => {

  let env = install(TRASH);
  await renderView(env, V.trash.renderTrash, {});
  assert.match(env.text(), /旧文件\.zip/);
  assert.match(env.text(), /清空回收站/);

  env = install({ '/api/file/trash/list': { files: [], total: '0' } });
  await renderView(env, V.trash.renderTrash, {});
  assert.match(env.text(), /回收站是空的/);
});

// ---------------------------------------------------------------- shares

const SHARES = {
  '/api/file/share/list': {
    items: [{
      shareCode: 'abc12345', fileName: '样品报告.pdf', fileSize: '2048', fileId: 'file-1',
      status: 'ACTIVE', accessCount: 3, maxAccessCount: 10,
      expireAt: '2099-01-01T00:00:00Z', shareUrl: '/s/abc12345', remark: '',
    }],
    total: '1',
    hasMore: false,
  },
};

test('shares：分享列表显示访问次数与有效期', async () => {
  let env = install(SHARES);
  await renderView(env, V.shares.renderShares, {});
  assert.match(env.text(), /样品报告\.pdf/);
  assert.match(env.text(), /有效/);
  assert.match(env.text(), /3/);

  env = install({ '/api/file/share/list': { items: [], total: '0', hasMore: false } });
  await renderView(env, V.shares.renderShares, {});
  assert.match(env.text(), /还没有创建过分享链接/);
});

// ---------------------------------------------------------------- offline

const OFFLINE = {
  '/api/download/offline/list': {
    records: [{
      id: 7, url: 'https://example.test/a.iso', fileName: 'a.iso', filePath: '/',
      state: 'DOWNLOAD_STATE_COMPLETED', fileSize: '1024', createdAt: ts, errorMessage: '',
    }],
    total: '1',
  },
};

test('offline：离线下载列表 / 空态', async () => {
  let env = install(OFFLINE);
  await renderView(env, V.offline.renderOffline, {});
  assert.match(env.text(), /a\.iso/);

  env = install({ '/api/download/offline/list': { records: [], total: '0' } });
  await renderView(env, V.offline.renderOffline, {});
  assert.match(env.text(), /还没有离线下载任务/);
});

// ---------------------------------------------------------------- transfers

const TRANSFERS = {
  '/api/upload/records/list': {
    records: [
      {
        id: 1, taskType: 'UPLOAD', status: 'FAILED',
        fileName: '坏文件.bin', fileSize: '512', message: 'temp file not found',
        createdAt: ts, targetProviderIds: '[1]',
      },
      {
        id: 2, taskType: 'BACKUP', status: 'COMPLETED',
        fileName: '站点备份.zip', fileSize: '4096', message: '',
        createdAt: ts, targetProviderIds: '[1]',
      },
    ],
    total: '2',
    failedCount: '1',
  },
};

test('transfers：失败任务可重试、目标存储源显示成名字', async () => {
  const env = install(TRANSFERS);
  await renderView(env, V.transfers.renderTransfers, {});
  const text = env.text();
  assert.match(text, /坏文件\.bin/);
  assert.match(text, /站点备份\.zip/);
  assert.match(text, /temp file not found/);
  assert.match(text, /本地/, 'provider 1 应当显示成名字，不是 id');
  assert.match(text, /重试全部失败（1）/);
  assert.match(text, /备份/, '任务类型要显示成中文');
});

test('transfers：切换状态筛选后列表仍然渲染（历史上这里会整页空白）', async () => {
  // 走迷你外壳：标签页会 navigate() 改 hash，必须真的经历 hashchange → 重新渲染
  const env = install(TRANSFERS);
  mountShell(env, { files: V.files.renderFiles, transfers: V.transfers.renderTransfers }, '#/transfers');
  await env.settle();
  const main = env.shell.main;
  assert.match(env.text(), /坏文件\.bin/);

  env.clickButton('失败', main);
  await env.settle();
  const afterFail = env.text();
  assert.match(afterFail, /传输队列/, '切筛选后页面骨架还在');
  assert.match(afterFail, /坏文件\.bin/, '筛选后记录不能消失');

  env.clickButton('全部', main);
  await env.settle();
  assert.match(env.text(), /站点备份\.zip/);
});

test('transfers：没有失败任务时不显示"重试全部"', async () => {
  const env = install({
    '/api/upload/records/list': {
      records: [{
        id: 2, taskType: 'UPLOAD', status: 'COMPLETED',
        fileName: 'ok.bin', fileSize: '1', createdAt: ts, targetProviderIds: '[1]',
      }],
      total: '1',
      failedCount: '0',
    },
  });
  await renderView(env, V.transfers.renderTransfers, {});
  assert.doesNotMatch(env.text(), /重试全部失败/);
});

// ---------------------------------------------------------------- webdav

const WEBDAV = {
  '/api/webdav/accounts/list': {
    davUrl: '/dav',
    accounts: [{
      id: 1, username: 'my-laptop', name: '笔记本', rootFileId: 'dir-1', rootPath: '/文档',
      rootValid: true, readOnly: true, enabled: true, disabledReason: '',
      createdTime: ts, lastUsedTime: '',
    }, {
      id: 2, username: 'broken', name: '', rootFileId: 'dir-x', rootPath: '/已删除',
      rootValid: false, readOnly: false, enabled: false, disabledReason: '绑定目录已失效',
      createdTime: ts, lastUsedTime: ts,
    }],
  },
};

test('webdav：账号列表包含只读标记与失效目录提示', async () => {
  let env = install(WEBDAV);
  await renderView(env, V.webdav.renderWebdav, {});
  const text = env.text();
  assert.match(text, /my-laptop/);
  assert.match(text, /只读/);
  assert.match(text, /\/dav/);
  assert.match(text, /绑定目录已失效/);
  assert.match(text, /从未使用/);

  env = install({ '/api/webdav/accounts/list': { davUrl: '/dav', accounts: [] } });
  await renderView(env, V.webdav.renderWebdav, {});
  assert.match(env.text(), /还没有 WebDAV 账号/);
});

// ---------------------------------------------------------------- admin: users

const ADMIN_USERS = {
  '/api/admin/user/list': {
    users: [
      { userId: 'u-1', displayName: 'smoke', email: 's@example.test', roles: ['admin'], createdAt: ts },
      { userId: 'u-2', displayName: 'li_test', email: 'l@example.test', roles: [], createdAt: ts },
    ],
    total: '2',
  },
  '/api/admin/user/spaces': {
    spaces: [{ userId: 'u-1', rootFileId: 'root-1', usedBytes: '1024', quotaBytes: '0', boundProviderIds: '[1]' }],
    total: '1',
  },
  '/api/admin/provider/list': {
    providers: [{ id: 1, name: '本地', providerType: 'LOCAL', enabled: true, description: '', defaultBound: true }],
    total: '1',
  },
};

test('admin users：没分配空间的用户显示"未分配"与分配入口', async () => {
  const env = install(ADMIN_USERS);
  await renderView(env, V.adminUsers.renderAdminUsers, {});
  const text = env.text();
  assert.match(text, /smoke/);
  assert.match(text, /li_test/);
  assert.match(text, /未分配/);
  assert.match(text, /分配根目录/);
  assert.match(text, /1 个已分配空间|已分配空间/);
});

test('admin users：配额展示按 GB 换算', async () => {
  const routes = structuredClone(ADMIN_USERS);
  routes['/api/admin/user/spaces'].spaces[0].quotaBytes = String(2 * 1024 ** 3);
  const env = install(routes);
  await renderView(env, V.adminUsers.renderAdminUsers, {});
  assert.match(env.text(), /2\.0 GB/);
});

// ---------------------------------------------------------------- admin: providers

const ADMIN_PROVIDERS = {
  '/api/admin/provider/list': {
    providers: [{
      id: 1, name: '本地存储', providerType: 'LOCAL', description: '默认', enabled: true,
      defaultBound: true, sortOrder: 0, totalCapacity: '0', usedCapacity: '1024',
      fileSizeLimit: '0', maxConcurrentUploads: 0, proxy: '',
    }],
    total: '1',
  },
  '/api/admin/provider/types': {
    types: [
      { type: 'LOCAL', displayName: '本地存储', configFields: [] },
      {
        type: 'PAN123', displayName: '123 云盘',
        configFields: [
          { key: 'refresh_token', label: '刷新令牌', fieldType: 'FIELD_TYPE_STRING', required: true, secret: true },
          { key: 'use_proxy', label: '走代理', fieldType: 'FIELD_TYPE_BOOLEAN', required: false },
        ],
      },
    ],
  },
};

test('admin providers：列表与默认绑定标记', async () => {
  const env = install(ADMIN_PROVIDERS);
  await renderView(env, V.adminProviders.renderAdminProviders, {});
  const text = env.text();
  assert.match(text, /本地存储/);
  assert.match(text, /默认绑定/);
  assert.match(text, /共 1 个存储源/);
});

test('admin providers：新增弹层的类型下拉按接口顺序排列', async () => {
  const env = install(ADMIN_PROVIDERS);
  const main = await renderView(env, V.adminProviders.renderAdminProviders, {});
  env.clickButton('＋ 新增存储源', main);
  await env.settle();
  const select = env.find('select');
  assert.ok(select, '应当渲染出类型下拉');
  assert.deepEqual(select.childNodes.map((o) => o.textContent), ['本地存储', '123 云盘']);
});

// ---------------------------------------------------------------- admin: backup

const ADMIN_BACKUP = {
  '/api/admin/backup/task/list': {
    tasks: [{
      id: 1, name: '站点备份', sourcePath: '/srv/site', targetDirId: 'root-1',
      strategy: 'BACKUP_STRATEGY_SCHEDULED', cronExpression: '0 0 * * *', enabled: true,
      recursive: true, backedUpCount: '2', backedUpSize: '20', lastRunAt: ts,
      lastRunStatus: 'BACKUP_RUN_STATUS_SUCCESS', lastRunMessage: '',
    }],
    total: '1',
  },
};

test('admin backup：任务列表显示策略 / 统计 / 源路径', async () => {
  let env = install(ADMIN_BACKUP);
  await renderView(env, V.adminBackup.renderAdminBackup, {});
  const text = env.text();
  assert.match(text, /站点备份/);
  assert.match(text, /\/srv\/site/);
  assert.match(text, /成功/);
  assert.match(text, /2 个 \/ 20 B/);
  assert.match(text, /定时/);

  env = install({ '/api/admin/backup/task/list': { tasks: [], total: '0' } });
  await renderView(env, V.adminBackup.renderAdminBackup, {});
  assert.match(env.text(), /还没有备份任务/);
});

test('admin backup：保存失败时显示服务端原文而不是"参数不正确"', async () => {
  const env = install({
    ...ADMIN_BACKUP,
    '/api/admin/backup/task/create': FAIL('PARAMS_ERROR', 'source path does not exist'),
  });
  const main = await renderView(env, V.adminBackup.renderAdminBackup, {});
  env.clickButton('＋ 新建任务', main);
  await env.settle();
  const inputs = env.document.querySelectorAll('.modal-body input');
  inputs[0].value = '新任务';
  inputs[1].value = '/srv/missing';
  env.document.querySelectorAll('.modal-foot button')
    .find((b) => b.textContent.trim() === '创建').click();
  await env.settle();
  assert.match(env.toasts.textContent, /source path does not exist/);
});

// ---------------------------------------------------------------- settings

const SETTINGS = {
  '/api/file/detail': {
    file: FOLDER({ id: 'dir-9', name: '图片', parentId: 'root-1', boundProviderIds: '[1]' }),
  },
  '/api/file/info': { file: FOLDER({ id: 'dir-9', name: '图片', parentId: 'root-1' }) },
};

test('settings：目录设置显示名称与绑定存储源', async () => {
  const env = install(SETTINGS);
  await renderView(env, V.settings.renderSettings, { params: { id: 'dir-9' } });
  const text = env.text();
  assert.match(text, /目录设置/);
  assert.match(text, /图片/);
  assert.match(text, /本地/, '绑定存储源应当显示名字');
  assert.match(text, /历史版本上限/);
});

// ---------------------------------------------------------------- detail

const DETAIL = {
  '/api/file/detail': { file: FILE(), locations: [], histories: [] },
  '/api/file/share/list': { items: [], total: '0' },
  '/api/file/sync-providers': { providers: [] },
};

test('detail：文件详情显示大小 / 哈希 / 位置区', async () => {
  const env = install(DETAIL);
  await renderView(env, V.detail.renderDetail, { params: { id: 'file-1' } });
  const text = env.text();
  assert.match(text, /报告\.pdf/);
  assert.match(text, /2\.0 KB|2048/);
  assert.match(text, /abcdef12/);
  assert.match(text, /位置|存储/, '应当有落地位置区块');
});

// ---------------------------------------------------------------- preview

const IMG = FILE({ id: 'img-1', name: '照片.jpg', type: 'IMAGE', mime: 'image/jpeg' });

test('preview：图片走票据 URL，带 from 时给出图集翻页', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': IMG,
    '/api/file/preview-ticket': { ticket: 'v1.payload.sig', expiresAt: '2099-01-01T00:00:00Z' },
    '/api/file/list': {
      files: [IMG, FILE({ id: 'img-2', type: 'IMAGE', name: 'b.jpg' }), FILE({ id: 'img-3', type: 'IMAGE', name: 'c.jpg' })],
      total: '3',
    },
  });
  await renderView(env, renderPreview, {
    name: 'preview', params: { id: 'img-1' }, query: new URLSearchParams('from=dir-1'),
  });
  const img = env.find('img');
  assert.ok(img, '应当渲染出 <img>');
  assert.match(img.getAttribute('src') || img.src, /ticket=v1\.payload\.sig/);
  assert.doesNotMatch(img.getAttribute('src') || img.src, /token=/);
  assert.match(env.text(), /1 \/ 3/, '图集进度');
  assert.match(env.text(), /滚轮缩放/);
});

test('preview：图片先出缩略图，「查看原图」才拉原图', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': IMG,
    '/api/file/preview-ticket': { ticket: 'v1.payload.sig', expiresAt: '2099-01-01T00:00:00Z' },
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'img-1' } });

  const img = env.find('img');
  const src = img.getAttribute('src') || img.src;
  // 默认是 400px 缩略图（本地磁盘，秒开），不是几 MB 的原图
  assert.match(src, /\/api\/file\/stream\/preview\?/);
  assert.match(src, /size=large/);
  assert.doesNotMatch(src, /\/api\/file\/preview\/raw\//);

  env.clickButton('查看原图');
  await env.settle();
  // 假 DOM 里 img.src = x 只改属性值（不反射到 attribute），所以这里读 property
  const after = env.find('img').src;
  assert.match(after, /\/api\/file\/preview\/raw\//);
  assert.doesNotMatch(env.text(), /查看原图/, '切到原图后按钮应消失');
});

test('preview：缓存后播放会把视频源换成 blob 并提示本机播放', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': FILE({ id: 'v-4', name: '录屏.mp4', type: 'VIDEO', mime: 'video/mp4' }),
    '/api/file/preview-ticket': { ticket: 'v1.t.s', expiresAt: '2099-01-01T00:00:00Z' },
    '/api/file/preview/raw/*': { $ranged: { total: 200 * 1024 } },
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'v-4' } });

  const before = env.find('video').src;
  assert.match(before, /\/api\/file\/preview\/raw\//, '默认播远端原文件');

  env.clickButton('缓存后播放（弱网更稳）');
  await env.settle(20);

  assert.match(env.find('video').src, /^blob:/, '缓存完成后应改播本机 blob');
  assert.match(env.text(), /从本机播放/);
});

test('preview：服务器不支持 Range 时缓存失败但保留直连播放', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': FILE({ id: 'v-5', name: '录屏.mp4', type: 'VIDEO', mime: 'video/mp4' }),
    '/api/file/preview-ticket': { ticket: 'v1.t.s', expiresAt: '2099-01-01T00:00:00Z' },
    // 200 = 服务器忽略 Range，预取应报错而不是把整份 body 当分片写坏
    '/api/file/preview/raw/*': HTTP(200, {}),
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'v-5' } });

  const before = env.find('video').src;
  env.clickButton('缓存后播放（弱网更稳）');
  await env.settle(20);

  assert.match(env.text(), /缓存失败/);
  assert.equal(env.find('video').src, before, '失败时不应改源');
});

test('preview：Markdown 走文本预览并渲染标题', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': FILE({ id: 'md-1', name: '说明.md', type: 'DOCUMENT' }),
    '/api/file/preview-ticket': { ticket: 'v1.t.s', expiresAt: '2099-01-01T00:00:00Z' },
    '/api/file/stream/preview*': { $blob: '# 标题\n\n正文内容' },
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'md-1' } });
  const text = env.text();
  assert.match(text, /标题/);
  assert.match(text, /正文内容/);
  assert.match(text, /文本/, '顶部标注"文本"');
});

test('preview：文档先等云端缓存就绪（202）再挂 Viewer', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  let hits = 0;
  const env = install({
    '/api/file/info': FILE({ id: 'pdf-1', name: '大书.pdf' }),
    '/api/file/preview-ticket': { ticket: 'v1.t.s', expiresAt: '2099-01-01T00:00:00Z' },
    // 第一次 202（Telegram 大文件正在缓存），第二次 206（已就绪）
    '/api/file/preview/raw/*': () => {
      hits += 1;
      return hits === 1 ? HTTP(202, { status: 'pre_download' }) : HTTP(206, {});
    },
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'pdf-1' } });
  assert.ok(hits >= 2, `202 之后应当重试探测，实际只请求了 ${hits} 次`);
  // 本环境没装 Viewer 资源 → 退回下载入口；关键是不能把 202 当成坏文件报错
  assert.match(env.text(), /未提供文档预览器/);
  assert.doesNotMatch(env.text(), /预览失败/);
});

test('preview：不能预览的类型跳到详情页', async () => {
  const { renderPreview } = await import('../js/views/preview.js');
  const env = install({
    '/api/file/info': FILE({ id: 'bin-1', name: 'a.bin', type: 'FILE_TYPE_UNKNOWN' }),
  });
  await renderView(env, renderPreview, { name: 'preview', params: { id: 'bin-1' } });
  assert.equal(env.location.hash, '#/detail/bin-1');
});

// ---------------------------------------------------------------- login

test('login：密码错误显示服务端提示，成功后清空密码', async () => {
  const env = install({
    '/api/auth/login': { $error: { status: 401, body: { code: 'UNAUTHORIZED', message: 'bad credentials' } } },
  });
  let ok = 0;
  V.login.renderLogin(env.root, () => { ok += 1; });
  await env.settle();
  const inputs = env.document.querySelectorAll('input');
  inputs[0].value = 'smoke';
  inputs[1].value = 'wrong';
  env.find('form').dispatchEvent(Object.assign(new V.fakeDom.FakeEvent('submit'), {}));
  await env.settle();
  assert.match(env.text(), /用户名或密码错误/);
  assert.equal(ok, 0);

  // 成功：返回 token，回调被调用
  const env2 = install({ '/api/auth/login': RAW({ token: 'jwt-token', name: 'smoke', isAdmin: true }) });
  let ok2 = 0;
  V.login.renderLogin(env2.root, () => { ok2 += 1; });
  await env2.settle();
  const inputs2 = env2.document.querySelectorAll('input');
  inputs2[0].value = 'smoke';
  inputs2[1].value = 'smoke123';
  env2.find('form').dispatchEvent(new V.fakeDom.FakeEvent('submit'));
  await env2.settle();
  assert.equal(ok2, 1);
  assert.equal(inputs2[1].value, '');
  assert.equal(env2.document.querySelectorAll('input')[0].value, 'smoke');
});
