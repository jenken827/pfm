// 跨页面共享的少量状态（根目录 / 存储源 / 本地偏好）。
//
// 不缓存文件列表：每次进入目录都重新拉，避免"删了文件列表还留着"的脏数据；
// 只有根目录 id 与存储源列表会缓存（一次会话内不变）。

import { call } from './api.js';

const VIEW_KEY = 'pfm_web_view';
const SORT_KEY = 'pfm_web_sort';

export const store = {
  rootId: '',
  space: null,
  providers: [],
  view: localStorage.getItem(VIEW_KEY) === 'grid' ? 'grid' : 'list',
  sortKey: localStorage.getItem(SORT_KEY) || 'name',
  sortDir: 1,
};

let spacePromise = null;

function noSpace(err) {
  err.noSpace = true;
  return err;
}
let providersPromise = null;

/** 取用户空间（含根目录 id），一次会话只请求一次 */
export function ensureSpace() {
  if (store.space) return Promise.resolve(store.space);
  if (!spacePromise) {
    spacePromise = call('/api/space/get', {}).then((data) => {
      const space = data.space || null;
      if (!space?.rootFileId) throw noSpace(new Error('服务端未配置用户根目录'));
      store.space = space;
      store.rootId = space.rootFileId;
      return space;
    }).catch((err) => {
      spacePromise = null;
      // 账号还没有分配根目录时后端返回 "user space not found"（PARAMS_ERROR）。
      // 打成标记，让外壳能给出"请管理员分配"这种能看懂的提示，
      // 而不是把统一的"参数不正确"甩给刚登录的人。
      if (!err.noSpace && /user space not found/i.test(err.rawMessage || err.message || '')) {
        noSpace(err);
      }
      throw err;
    });
  }
  return spacePromise;
}

/**
 * 重新拉一次用户空间（容量会随删除 / 离线入库 / 回收站恢复变化）。
 * 更新后广播 `pfm:space`，侧边栏的容量条据此重绘。
 */
export async function refreshSpace() {
  try {
    const data = await call('/api/space/get', {});
    const space = data.space || null;
    if (!space) return store.space;
    store.space = space;
    if (space.rootFileId) store.rootId = space.rootFileId;
    window.dispatchEvent(new Event('pfm:space'));
    return space;
  } catch {
    return store.space; // 容量显示失败不影响主流程
  }
}

/** 取"我的可用存储源"，供位置 / 目录绑定选择器使用 */
export function loadProviders() {
  if (store.providers.length) return Promise.resolve(store.providers);
  if (!providersPromise) {
    providersPromise = call('/api/space/my-providers', {}).then((data) => {
      store.providers = data.providers || [];
      return store.providers;
    }).catch((err) => {
      providersPromise = null;
      throw err;
    });
  }
  return providersPromise;
}

export function setView(view) {
  store.view = view === 'grid' ? 'grid' : 'list';
  localStorage.setItem(VIEW_KEY, store.view);
}

export function setSort(key) {
  store.sortKey = key;
  localStorage.setItem(SORT_KEY, key);
}

/** 根目录 id 的同步便捷读取（未加载时返回空串） */
export function rootId() {
  return store.rootId;
}
