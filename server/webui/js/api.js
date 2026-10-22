// REST 客户端 + 文件字节 URL 构造。
//
// 两类请求的凭证完全不同，不要混：
//   - 元数据（列表 / 详情 / 改名 / 删除 …）：Authorization: Bearer <JWT>；
//   - 文件字节（图片 / 文本 / 音视频 / Viewer / 下载）：?ticket=<单文件票据>。
//     浏览器直接发起的请求（<img> / <video> / iframe / <a download>）无法带请求头，
//     而 access token 是账户级 30 天凭证，绝不能出现在 URL 里。

import { session, expireSession } from './session.js';

// 票据用途：预览 10 分钟，下载 12 小时（浏览器下载器续传要重新校验）
export const PURPOSE_PREVIEW = 'PREVIEW';
export const PURPOSE_DOWNLOAD = 'DOWNLOAD';

const CODE_MESSAGE = {
  TOKEN_INVALID: '登录已过期，请重新登录',
  PARAMS_ERROR: '参数不正确',
  NOT_FOUND: '文件不存在或已被删除',
  ALREADY_EXISTS: '已经存在同名项',
  PERMISSION_DENIED: '没有权限执行该操作',
  INTERNAL_ERROR: '服务端内部错误',
  DB_ERROR: '数据库错误',
  RATE_LIMIT_EXCEEDED: '操作太频繁，请稍后再试',
  PARTIAL_UPLOAD_ERROR: '部分上传失败',
  DIRECT_LINK_UNAVAILABLE: '该文件不支持直链，请改用下载',
};

export class ApiError extends Error {
  constructor(message, code = '', rawMessage = '') {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    // 服务端原始 message。业务错误被 CODE_MESSAGE 统一成中文短语后，像
    // "source path does not exist" 这种定位信息就丢了；需要细节的视图可以显式用它。
    this.rawMessage = rawMessage || '';
  }
}

function authHeaders(extra) {
  const headers = { ...extra };
  if (session.token) headers.Authorization = `Bearer ${session.token}`;
  return headers;
}

/**
 * 调用 REST 业务接口；成功返回 BaseResp.data（已去掉 @type 包装的字段对象）。
 *
 * 业务错误是 HTTP 200 + code != SUCCESS；401 是鉴权失败（token 失效）。
 */
export async function call(path, body = {}) {
  let res;
  try {
    res = await fetch(path, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body || {}),
    });
  } catch {
    throw new ApiError('网络请求失败，请检查连接');
  }

  if (res.status === 401) {
    expireSession();
    throw new ApiError('登录已过期，请重新登录', 'TOKEN_INVALID');
  }

  let json = null;
  try {
    json = await res.json();
  } catch {
    json = null;
  }

  if (!res.ok) {
    throw new ApiError(json?.message || `请求失败（HTTP ${res.status}）`, json?.code || '', json?.message || '');
  }
  const code = json?.code;
  if (code === 'TOKEN_INVALID') {
    expireSession();
    throw new ApiError(CODE_MESSAGE.TOKEN_INVALID, code);
  }
  if (code && code !== 'SUCCESS' && code !== 1) {
    throw new ApiError(CODE_MESSAGE[code] || json?.message || `错误码 ${code}`, code, json?.message || '');
  }
  return json?.data || {};
}

/**
 * 换一张单文件票据。
 * @param {string} fileId
 * @param {'PREVIEW'|'DOWNLOAD'} purpose 决定有效期（10 分钟 / 12 小时）
 * @returns {Promise<{ticket: string, expiresAt: string}>}
 */
export async function ticket(fileId, purpose = PURPOSE_PREVIEW) {
  const data = await call('/api/file/preview-ticket', { fileId, purpose });
  if (!data.ticket) throw new ApiError('服务端未返回票据');
  return { ticket: data.ticket, expiresAt: data.expiresAt || '' };
}

/** 下载地址（12 小时票据；浏览器下载器续传时会重新校验它） */
export async function downloadUrl(fileId, { providerId } = {}) {
  const { ticket: t } = await ticket(fileId, PURPOSE_DOWNLOAD);
  return buildDownloadUrl(fileId, t, providerId);
}

export function buildDownloadUrl(fileId, t, providerId) {
  const q = new URLSearchParams({ file_id: fileId, ticket: t });
  if (providerId !== undefined && providerId !== null && providerId !== '') {
    q.set('provider_id', String(providerId));
  }
  return `/api/file/stream/download?${q.toString()}`;
}

/** 历史版本下载地址（票据必须绑定宿主文件，见后端 GetFileHistoryStream） */
export async function historyDownloadUrl(historyId, fileId) {
  const { ticket: t } = await ticket(fileId, PURPOSE_DOWNLOAD);
  return buildHistoryUrl('download', historyId, t);
}

/** 历史版本预览地址 */
export async function historyPreviewUrl(historyId, fileId) {
  const { ticket: t } = await ticket(fileId, PURPOSE_PREVIEW);
  return buildHistoryUrl('preview', historyId, t);
}

function buildHistoryUrl(kind, historyId, t) {
  const q = new URLSearchParams({ id: String(historyId), ticket: t });
  return `/api/file/history/${kind}?${q.toString()}`;
}

/**
 * 原始文件地址（Viewer / <video> / <audio> 的 src）。
 *
 * 把文件名放进路径：Viewer 靠扩展名选渲染器，比单看 Content-Type 可靠。
 */
export function rawPreviewUrl(fileId, fileName, t) {
  const name = encodeURIComponent(fileName || 'file');
  const q = new URLSearchParams({ file_id: fileId, ticket: t });
  return `/api/file/preview/raw/${name}?${q.toString()}`;
}

/**
 * 缩略图地址（<img src> 用，票据走 query，见 server/middleware 的票据校验）。
 *
 * 原图动辄几 MB，而缩略图是本地磁盘上的 JPEG（200/400 px），首屏用它秒开；
 * 用户要看细节时再换成 rawPreviewUrl 的原图。
 */
export function thumbPreviewUrl(fileId, t, size = 'large') {
  const q = new URLSearchParams({ file_id: fileId, ticket: t, size });
  return `/api/file/stream/preview?${q.toString()}`;
}

/** 直链 302（Location 里自带票据）：给浏览器/第三方下载器用 */
export function directUrl(fileId, t, providerId) {
  const q = new URLSearchParams({ file_id: fileId, ticket: t });
  if (providerId !== undefined && providerId !== null && providerId !== '') {
    q.set('provider_id', String(providerId));
  }
  return `/api/file/stream/direct?${q.toString()}`;
}

/** 缩略图 / 原图（带 header 鉴权，用 fetch + Blob 加载） */
export function previewPath(fileId, { original = false, size = '', providerId } = {}) {
  const q = new URLSearchParams({ file_id: fileId });
  if (original) q.set('original', 'true');
  if (size) q.set('size', size);
  if (providerId !== undefined && providerId !== null && providerId !== '') {
    q.set('provider_id', String(providerId));
  }
  return `/api/file/stream/preview?${q.toString()}`;
}

/**
 * 用 Authorization 头取二进制（缩略图 / 文本片段）。
 *
 * 返回 Blob；HTTP 202 表示"云端正在准备"（Telegram 大文件），调用方据此提示重试。
 */
export async function fetchBlob(path, { range } = {}) {
  const headers = authHeaders();
  if (range) headers.Range = range;
  const res = await fetch(path, { headers });
  if (res.status === 202) {
    let info = {};
    try {
      info = await res.json();
    } catch {
      info = {};
    }
    const err = new ApiError(info.message || '云端正在准备该文件，请稍后重试', 'PRE_DOWNLOAD');
    err.preDownload = info;
    throw err;
  }
  if (res.status === 401) {
    expireSession();
    throw new ApiError('登录已过期，请重新登录', 'TOKEN_INVALID');
  }
  if (!res.ok) {
    let msg = `读取失败（HTTP ${res.status}）`;
    try {
      msg = (await res.text()) || msg;
    } catch {
      /* 保持默认文案 */
    }
    throw new ApiError(msg);
  }
  return res.blob();
}
