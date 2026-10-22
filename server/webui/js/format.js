// 格式化与展示用常量。所有文案与客户端的口径保持一致，便于两端对照。

const SIZE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** 人类可读的体积；0 / 未知显示 '-' */
export function fmtSize(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return '-';
  let v = n;
  let i = 0;
  while (v >= 1024 && i < SIZE_UNITS.length - 1) {
    v /= 1024;
    i += 1;
  }
  const text = i === 0 ? String(v) : v.toFixed(v >= 100 ? 0 : 1);
  return `${text} ${SIZE_UNITS[i]}`;
}

function toDate(value) {
  if (!value) return null;
  // proto3 JSON 的时间戳是 RFC3339 字符串；未设置时是 null
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d;
}

const pad = (n) => (n < 10 ? `0${n}` : String(n));

/** 'YYYY-MM-DD HH:mm'（本地时区） */
export function fmtTime(value) {
  const d = toDate(value);
  if (!d) return '-';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** 'YYYY-MM-DD' */
export function fmtDate(value) {
  const d = toDate(value);
  if (!d) return '-';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** 相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前 / 日期 */
export function relTime(value) {
  const d = toDate(value);
  if (!d) return '-';
  const diff = Date.now() - d.getTime();
  if (diff < 60_000) return '刚刚';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 30 * 86_400_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return fmtDate(value);
}

/** 剩余有效期（分享链接 / 直链） */
export function fmtRemain(value) {
  const d = toDate(value);
  if (!d) return '永不过期';
  const diff = d.getTime() - Date.now();
  if (diff <= 0) return '已过期';
  if (diff < 3_600_000) return `${Math.ceil(diff / 60_000)} 分钟后过期`;
  if (diff < 86_400_000) return `${Math.ceil(diff / 3_600_000)} 小时后过期`;
  return `${Math.ceil(diff / 86_400_000)} 天后过期`;
}

export const TYPE_LABEL = {
  FILE_TYPE_UNKNOWN: '其他',
  FOLDER: '文件夹',
  IMAGE: '图片',
  VIDEO: '视频',
  AUDIO: '音频',
  DOCUMENT: '文档',
  ARCHIVE: '压缩包',
  FONT: '字体',
  APPLICATION: '应用',
};

export const TYPE_ICON = {
  FOLDER: '📁',
  IMAGE: '🖼️',
  VIDEO: '🎬',
  AUDIO: '🎵',
  DOCUMENT: '📄',
  ARCHIVE: '🗜️',
  FONT: '🔤',
  APPLICATION: '📦',
  FILE_TYPE_UNKNOWN: '📄',
};

export function isDir(file) {
  return file?.type === 'FOLDER';
}

export function iconFor(file) {
  return TYPE_ICON[file?.type] || TYPE_ICON.FILE_TYPE_UNKNOWN;
}

export function typeText(type) {
  return TYPE_LABEL[type] || '其他';
}

const LOC_STATUS = {
  UPLOAD_PROVIDER_STATUS_UNSPECIFIED: '未知',
  PPENDING: '待上传',
  PUPLOADING: '上传中',
  PCOMPLETED: '已完成',
  PFAILED: '失败',
  PDELETED_FAILED: '删除失败',
};

const CACHE_STATUS = {
  CACHE_STATUS_UNSPECIFIED: '未知',
  UNCACHED: '未缓存',
  CACHING: '缓存中',
  CACHED: '已缓存',
};

const SHARE_STATUS = {
  SHARE_STATUS_UNSPECIFIED: '未知',
  ACTIVE: '有效',
  CANCELLED: '已取消',
  EXPIRED: '已过期',
};

const DOWNLOAD_STATE = {
  DOWNLOAD_RECORD_STATE_UNSPECIFIED: '未知',
  DOWNLOAD_PENDING: '排队中',
  DOWNLOADING: '下载中',
  DOWNLOAD_COMPLETED: '已完成',
  DOWNLOAD_FAILED: '失败',
};

/** 状态 → [文案, 样式修饰符] */
export function locStatus(s) {
  return [LOC_STATUS[s] || '未知', s === 'PCOMPLETED' ? 'ok' : s === 'PFAILED' ? 'bad' : ''];
}

export function cacheStatus(s) {
  return CACHE_STATUS[s] || '未知';
}

export function shareStatus(s) {
  return [SHARE_STATUS[s] || '未知', s === 'ACTIVE' ? 'ok' : s === 'EXPIRED' ? 'warn' : ''];
}

export function downloadState(s) {
  return [DOWNLOAD_STATE[s] || '未知', s === 'DOWNLOAD_COMPLETED' ? 'ok' : s === 'DOWNLOAD_FAILED' ? 'bad' : ''];
}

/** 文件名比较：数字感知，中文按拼音（localeCompare 的 zh 排序） */
export function compareName(a, b) {
  return String(a?.name || '').localeCompare(String(b?.name || ''), 'zh-Hans-CN', { numeric: true });
}

const SORTERS = {
  name: compareName,
  size: (a, b) => Number(a?.size || 0) - Number(b?.size || 0),
  time: (a, b) => new Date(a?.updatedAt || a?.createdAt || 0) - new Date(b?.updatedAt || b?.createdAt || 0),
  type: (a, b) => String(a?.type || '').localeCompare(String(b?.type || '')),
};

/**
 * 排序：文件夹恒在前，同类按 key 排序。
 * dir = 1 升序 / -1 降序；文件夹始终升序（与客户端一致，避免"倒序把文件夹堆到末尾"）。
 */
export function sortFiles(files, key = 'name', dir = 1) {
  const cmp = SORTERS[key] || compareName;
  const dirs = files.filter(isDir).sort(compareName);
  const rest = files.filter((f) => !isDir(f)).sort((a, b) => cmp(a, b) * dir);
  return [...dirs, ...rest];
}

/** 截断长 hash / code 用于展示 */
export function shortHash(hash, n = 16) {
  if (!hash) return '-';
  const s = String(hash);
  return s.length <= n ? s : `${s.slice(0, n)}…`;
}
