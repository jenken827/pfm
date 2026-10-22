// 文件详情页：客户端「文件详情」的浏览器对应版本。
//
// 一页放齐所有"针对单个文件"的操作：概览、位置分布（按源下载 / 取直链）、
// 历史版本（预览 / 下载 / 恢复 / 删除）、标签、分享链接、同步到其它存储源。

import {
  call, downloadUrl, historyDownloadUrl, historyPreviewUrl, directUrl, ticket,
  PURPOSE_DOWNLOAD,
} from '../api.js';
import { h, toast, confirmDialog, promptDialog, openModal, copyText, triggerDownload, setChildren } from '../dom.js';
import {
  fmtSize, fmtTime, isDir, typeText, locStatus, cacheStatus,
  shareStatus, shortHash, fmtRemain,
} from '../format.js';
import { navigate, back } from '../router.js';
import { store } from '../store.js';
import { previewKindOf } from './preview_kind.js';
import { parseProviderIds } from '../components/provider_ids.js';

let renderToken = 0;

export async function renderDetail(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';
  setChildren(main, h('div', { class: 'empty' }, h('div', { class: 'text', text: '正在加载…' })));

  let data;
  try {
    data = await call('/api/file/detail', { fileId: route.params.id });
  } catch (err) {
    if (!alive()) return;
    setChildren(main, h('div', { class: 'empty' },
      h('div', { class: 'error-text', text: `加载失败：${err.message}` }),
      h('div', { style: { marginTop: '12px' } },
        h('button', { text: '返回', onclick: () => back('/files') }))));
    return;
  }
  if (!alive()) return;

  const file = data.file;
  if (!file) {
    setChildren(main, h('div', { class: 'empty', text: '文件不存在或已被删除' }));
    return;
  }
  if (isDir(file)) {
    navigate(`/files/${encodeURIComponent(file.id)}`, { replace: true });
    return;
  }

  const reload = () => renderDetail(main, route);
  const head = h('div', { class: 'page-head' },
    h('button', { class: 'ghost', text: '← 返回', onclick: () => back(`/files/${encodeURIComponent(file.parentId || store.rootId)}`) }),
    h('h1', { text: file.name || '(未命名)' }),
    h('div', { class: 'spacer' }),
    previewKindOf(file) !== 'detail'
      ? h('button', { class: 'primary', text: '预览', onclick: () => navigate(`/preview/${encodeURIComponent(file.id)}`) })
      : null,
    h('button', { text: '下载', onclick: () => downloadFile(file) }));

  const body = h('div', { class: 'detail-grid' },
    h('div', null,
      metaCard(file, data, { onReload: reload }),
      tagsCard(file, data.tags || [], reload)),
    h('div', null,
      locationsCard(file, data.locations || []),
      historiesCard(file, data.histories || [], reload),
      shareCard(file)));

  setChildren(main, h('div', { class: 'page' }, head, body));
}

async function downloadFile(file, providerId) {
  try {
    const url = await downloadUrl(file.id, { providerId });
    triggerDownload(url);
  } catch (err) {
    toast(`下载失败：${err.message}`, 'err');
  }
}

// ---------------------------------------------------------------- 概览

function metaRow(dl, label, value) {
  dl.append(h('dt', { text: label }), h('dd', { text: value }));
  return dl;
}

function metaCard(file, data, { onReload }) {
  const dl = h('dl', { class: 'meta' });
  metaRow(dl, '类型', typeText(file.type));
  metaRow(dl, '大小', fmtSize(file.size));
  metaRow(dl, '修改时间', fmtTime(file.updatedAt || file.createdAt));
  metaRow(dl, '创建时间', fmtTime(file.createdAt));
  metaRow(dl, '就绪', file.isReady === false ? '否（尚未上传完成）' : '是');
  if (file.mime) metaRow(dl, 'MIME', file.mime);
  if (file.hash) {
    // 完整哈希 64 位放不进这一列：显示前 16 位，点一下复制全文（含悬停提示）
    const dd = h('dd', {
      class: 'copyable',
      text: shortHash(file.hash, 16),
      title: `${file.hash}\n点击复制`,
      onclick: () => copyText(file.hash).then(() => toast('哈希已复制', 'ok')).catch(() => {}),
    });
    dl.append(h('dt', { text: '哈希' }), dd);
  }
  metaRow(dl, '加密', file.enableEncryption ? '是' : '否');
  // 历史版本上限是**目录**级设置（见 settings.js / 客户端 FolderSettingsDialog），
  // 目录详情会重定向到目录列表，这里不显示；文件上显示只会是 0，读起来像"不限制"，反而误导。
  // bound_provider_ids 是 JSON 数组字符串；空数组表示继承父目录的绑定
  if (file.type === 'FOLDER') {
    const ids = parseProviderIds(file.boundProviderIds);
    metaRow(dl, '绑定存储源', ids.length ? ids.map((id) => `#${id}`).join('、') : '继承父目录');
  }

  const actions = h('div', { class: 'toolbar', style: { marginTop: '12px', marginBottom: '0' } },
    h('button', { class: 'sm', text: '取直链', title: '存储提供者签发的临时直链，只写入剪贴板', onclick: (ev) => copyDirectLink(file.id, null, ev.target) }),
    h('button', { class: 'sm', text: '重命名', onclick: () => renameFile(file, onReload) }),
    h('button', { class: 'sm', text: '移动', onclick: () => moveFile(file, onReload) }),
    h('button', { class: 'sm', text: '同步到其它源', onclick: () => syncToProviders(file) }),
    h('button', { class: 'sm danger', text: '删除', onclick: () => deleteFile(file) }));

  return h('div', { class: 'section' }, h('h3', { text: '概览' }), dl, actions);
}

async function renameFile(file, onReload) {
  const name = await promptDialog({
    title: '重命名',
    label: '新名称',
    value: file.name || '',
    okText: '保存',
    validate: (v) => (v ? '' : '名称不能为空'),
  });
  if (!name) return;
  try {
    await call('/api/file/rename', { renameObjs: [{ id: file.id, newName: name }] });
    toast('已重命名', 'ok');
    onReload();
  } catch (err) {
    toast(`重命名失败：${err.message}`, 'err');
  }
}

async function moveFile(file, onReload) {
  const { pickFolder } = await import('../components/picker.js');
  const picked = await pickFolder({ title: '移动到' });
  const target = picked?.id || '';
  if (!target) return;
  try {
    await call('/api/file/move', { fileIds: [file.id], targetParentId: target });
    toast('已移动', 'ok');
    onReload();
  } catch (err) {
    toast(`移动失败：${err.message}`, 'err');
  }
}

async function deleteFile(file) {
  const ok = await confirmDialog(`确定把「${file.name}」移到回收站吗？`, {
    title: '移入回收站',
    okText: '删除',
    danger: true,
  });
  if (!ok) return;
  try {
    await call('/api/file/delete', { ids: [file.id] });
    toast('已移入回收站', 'ok');
    navigate(`/files/${encodeURIComponent(file.parentId || store.rootId)}`);
  } catch (err) {
    toast(`删除失败：${err.message}`, 'err');
  }
}

/** 直链只进剪贴板：不写 DOM 文本、不进 href、不进历史记录 */
async function copyDirectLink(fileId, providerId, node) {
  const prev = node?.textContent;
  if (node) node.textContent = '取直链…';
  try {
    const { ticket: t } = await ticket(fileId, PURPOSE_DOWNLOAD);
    const url = directUrl(fileId, t, providerId);
    // 直链 302 的 Location 由服务端现签，这里只把入口写上剪贴板
    await copyText(new URL(url, location.origin).href);
    toast('直链已复制（浏览器/下载器直接访问即可）', 'ok');
  } catch (err) {
    toast(`该文件不支持直链：${err.message}`, 'err');
  } finally {
    if (node) node.textContent = prev;
  }
}

// ---------------------------------------------------------------- 位置分布

function locationsCard(file, locations) {
  const rows = locations.map((loc) => {
    const [text, tone] = locStatus(loc.status);
    const done = loc.status === 'PCOMPLETED';
    const bits = [];
    if (loc.providerName) bits.push(loc.providerName);
    if (loc.providerType) bits.push(loc.providerType);
    if (loc.size) bits.push(`存储大小 ${fmtSize(loc.size)}`);
    if (loc.encrypted) bits.push('已加密');
    if (loc.isChunked) bits.push(`分片 ×${loc.chunkTotal || '?'}`);
    if (loc.cacheStatus) bits.push(`缓存 ${cacheStatus(loc.cacheStatus)}`);
    if (loc.storagePath) bits.push(loc.storagePath);

    return h('div', { class: 'rowitem' },
      h('span', { class: 'grow', text: `#${loc.providerId || '?'} ${bits.join(' · ')}` }),
      h('span', { class: `status-chip ${tone}`.trim(), text }),
      done ? h('span', {
        class: 'linklike',
        text: '从此源下载',
        onclick: () => downloadFile(file, loc.providerId),
      }) : null,
      done ? h('span', {
        class: 'linklike',
        text: '直链',
        onclick: (ev) => copyDirectLink(file.id, loc.providerId, ev.target),
      }) : null);
  });

  return h('div', { class: 'section' },
    h('h3', { text: `位置分布（${locations.length}）` }),
    rows.length ? rows : h('div', { class: 'muted small', text: '暂无位置信息' }));
}

// ---------------------------------------------------------------- 历史版本

function historiesCard(file, histories, reload) {
  const rows = histories.map((hist) => {
    const id = String(hist.id);
    return h('div', { class: 'rowitem' },
      h('span', { class: 'grow', text: `${fmtTime(hist.versionNumber || hist.createdAt)} · ${fmtSize(hist.size)}` }),
      h('span', {
        class: 'linklike',
        text: '预览',
        onclick: () => openHistory('preview', id, file),
      }),
      h('span', {
        class: 'linklike',
        text: '下载',
        onclick: () => openHistory('download', id, file),
      }),
      h('span', {
        class: 'linklike',
        text: '恢复',
        onclick: () => restoreHistory(id, file, reload),
      }),
      h('span', {
        class: 'linklike',
        text: '删除',
        onclick: () => deleteHistory(id, reload),
      }),
      hist.hash ? h('span', { class: 'sub', text: `hash ${shortHash(hist.hash)}` }) : null);
  });

  return h('div', { class: 'section' },
    h('h3', { text: `历史版本（${histories.length}）` }),
    rows.length ? rows : h('div', { class: 'muted small', text: '无历史版本' }));
}

async function openHistory(kind, historyId, file) {
  try {
    const url = kind === 'preview'
      ? await historyPreviewUrl(historyId, file.id)
      : await historyDownloadUrl(historyId, file.id);
    if (kind === 'download') triggerDownload(url);
    else openUrlModal(file.name, url);
  } catch (err) {
    toast(`打开失败：${err.message}`, 'err');
  }
}

function openUrlModal(title, url) {
  const handle = openModal({
    title: `历史版本 · ${title}`,
    wide: true,
    body: h('iframe', { src: url, style: { width: '100%', height: '70vh', border: '0' } }),
    footer: [h('button', { text: '关闭', onclick: () => handle.close() })],
  });
}

async function restoreHistory(historyId, file, reload) {
  const name = await promptDialog({
    title: '恢复历史版本',
    label: '恢复后的文件名（留空则自动命名）',
    value: '',
    placeholder: `${file.name} (恢复)`,
    okText: '恢复',
    validate: () => '',
  });
  if (name === null) return;
  try {
    const body = { historyId };
    if (name) body.newName = name;
    await call('/api/file/history/restore', body);
    toast('已恢复为当前版本', 'ok');
    reload();
  } catch (err) {
    toast(`恢复失败：${err.message}`, 'err');
  }
}

async function deleteHistory(historyId, reload) {
  const ok = await confirmDialog('删除这个历史版本？删除后无法恢复。', {
    title: '删除历史版本',
    okText: '删除',
    danger: true,
  });
  if (!ok) return;
  try {
    await call('/api/file/history/delete', { ids: [String(historyId)] });
    toast('已删除', 'ok');
    reload();
  } catch (err) {
    toast(`删除失败：${err.message}`, 'err');
  }
}

// ---------------------------------------------------------------- 标签

function tagsCard(file, tags, reload) {
  const list = h('div', { class: 'tags' });
  const input = h('input', {
    type: 'text',
    placeholder: '输入标签后回车',
    style: { width: '180px' },
  });

  const save = async (next) => {
    try {
      await call('/api/file/tags/update', { fileId: file.id, tags: next });
      toast('标签已更新', 'ok');
      reload();
    } catch (err) {
      toast(`保存失败：${err.message}`, 'err');
    }
  };

  tags.forEach((tag) => {
    list.appendChild(h('span', { class: 'tag' },
      tag,
      h('span', {
        class: 'x',
        text: '✕',
        onclick: () => save(tags.filter((t) => t !== tag)),
      })));
  });
  if (!tags.length) list.appendChild(h('span', { class: 'muted small', text: '还没有标签' }));

  input.addEventListener('keydown', (ev) => {
    if (ev.key !== 'Enter') return;
    ev.preventDefault();
    const value = input.value.trim();
    if (!value) return;
    if (tags.includes(value)) {
      toast('标签已存在');
      return;
    }
    save([...tags, value]);
  });

  return h('div', { class: 'section' },
    h('h3', { text: '标签' }),
    list,
    h('div', { class: 'toolbar', style: { marginTop: '10px', marginBottom: '0' } }, input));
}

// ---------------------------------------------------------------- 分享

function shareCard(file) {
  const container = h('div', { class: 'section' });
  const listBox = h('div');
  container.append(h('h3', { text: '分享链接' }), listBox);
  container.appendChild(h('button', {
    class: 'sm',
    text: '＋ 新建分享链接',
    style: { marginTop: '10px' },
    onclick: () => createShare(file, container),
  }));
  // 列表异步填充：不阻塞详情页其它区块的渲染
  loadShares(file, listBox);
  return container;
}

async function loadShares(file, listBox) {
  try {
    const data = await call('/api/file/share/list', { offset: 0, pageSize: 200 });
    const items = (data.items || []).filter((it) => it.fileId === file.id);
    if (!items.length) {
      listBox.appendChild(h('div', { class: 'muted small', text: '还没有分享链接' }));
      return;
    }
    items.forEach((item) => listBox.appendChild(shareRow(item, listBox)));
  } catch (err) {
    listBox.appendChild(h('div', { class: 'error-text small', text: `读取分享失败：${err.message}` }));
  }
}

function shareRow(item, container) {
  const [statusText, tone] = shareStatus(item.status);
  return h('div', { class: 'rowitem' },
    h('span', { class: 'grow one-line', text: item.shareUrl || item.shareCode, title: item.shareUrl || '' }),
    h('span', { class: `status-chip ${tone}`.trim(), text: statusText }),
    h('span', { class: 'muted small', text: fmtRemain(item.expireAt) }),
    item.accessCount ? h('span', { class: 'muted small', text: `访问 ${item.accessCount}` }) : null,
    // 已取消 / 已过期的链接打开只会 404，不给入口（复制仍保留，方便留档比对）
    item.status === 'ACTIVE' ? h('span', {
      class: 'linklike',
      text: '打开',
      onclick: () => window.open(item.shareUrl, '_blank', 'noopener'),
    }) : null,
    h('span', {
      class: 'linklike',
      text: '复制',
      onclick: async () => {
        try {
          await copyText(item.shareUrl);
          toast('链接已复制', 'ok');
        } catch (err) {
          toast(err.message, 'err');
        }
      },
    }),
    item.status === 'ACTIVE' ? h('span', {
      class: 'linklike',
      text: '取消',
      onclick: async () => {
        const ok = await confirmDialog('取消后该链接立即失效，确定吗？', { title: '取消分享', danger: true, okText: '取消分享' });
        if (!ok) return;
        try {
          await call('/api/file/share/cancel', { shareCode: item.shareCode });
          toast('已取消分享', 'ok');
          // 分享状态变了，重进详情页拿最新列表
          navigate(`/detail/${encodeURIComponent(item.fileId)}`);
        } catch (err) {
          toast(`取消失败：${err.message}`, 'err');
        }
      },
    }) : null);
}

async function createShare(file, container) {
  const expire = h('select', null,
    h('option', { value: '86400', text: '1 天' }),
    h('option', { value: '604800', text: '7 天', selected: true }),
    h('option', { value: '2592000', text: '30 天' }),
    h('option', { value: 'never', text: '永久有效' }));
  const maxAccess = h('input', { type: 'text', placeholder: '留空表示不限' });
  const remark = h('input', { type: 'text', placeholder: '备注（可选）' });

  let done = false;
  const finish = (v) => {
    if (done) return;
    done = true;
    handle.close();
  };
  const handle = openModal({
    title: '新建分享链接',
    body: [
      h('div', { class: 'field' }, h('label', { text: '有效期' }), expire),
      h('div', { class: 'field' }, h('label', { text: '最多访问次数' }), maxAccess),
      h('div', { class: 'field' }, h('label', { text: '备注' }), remark),
      h('div', { class: 'hint', text: '分享链接是公开的：任何拿到链接的人都能下载这个文件。' }),
    ],
    footer: [
      h('button', { text: '取消', onclick: () => finish(null) }),
      h('button', {
        class: 'primary',
        text: '创建',
        onclick: async (ev) => {
          const body = { fileId: file.id };
          if (expire.value === 'never') body.neverExpire = true;
          else body.expireSeconds = Number(expire.value);
          if (maxAccess.value.trim()) {
            const n = Number(maxAccess.value.trim());
            if (!Number.isInteger(n) || n <= 0) {
              toast('最多访问次数要填正整数，留空表示不限', 'err');
              return;
            }
            body.maxAccessCount = n;
          }
          if (remark.value.trim()) body.remark = remark.value.trim();
          ev.target.disabled = true;
          try {
            const data = await call('/api/file/share/create', body);
            finish(data);
            toast('分享链接已创建', 'ok');
            try {
              await copyText(data.shareUrl);
              toast('链接已复制到剪贴板', 'ok');
            } catch {
              /* 复制失败也仍然展示在列表里 */
            }
            navigate(`/detail/${encodeURIComponent(file.id)}`);
          } catch (err) {
            toast(`创建失败：${err.message}`, 'err');
            ev.target.disabled = false;
          }
        },
      }),
    ],
    onClose: () => finish(null),
  });
}

// ---------------------------------------------------------------- 同步到其它源

async function syncToProviders(file) {
  if (!file.hash) {
    toast('该文件缺少哈希，无法同步', 'err');
    return;
  }
  let providers = [];
  try {
    const data = await call('/api/file/sync-providers', { fileHash: file.hash });
    providers = data.providers || [];
  } catch (err) {
    toast(`读取可用存储源失败：${err.message}`, 'err');
    return;
  }
  if (!providers.length) {
    toast('没有其它可用的存储源');
    return;
  }

  const boxes = providers.map((p) => h('input', { type: 'checkbox', value: String(p.id) }));
  let done = false;
  const finish = () => {
    if (done) return;
    done = true;
    handle.close();
  };
  const handle = openModal({
    title: '同步到其它存储源',
    body: h('div', null,
      h('p', { class: 'muted small', text: '把该文件的字节再写一份到选中的存储源（不改变原位置）。' }),
      providers.map((p, i) => h('label', { class: 'rowitem', style: { cursor: 'pointer' } },
        h('label', { class: 'check' }, boxes[i], h('span', { text: `#${p.id} ${p.name || ''}（${p.providerType || ''}）` }))))),
    footer: [
      h('button', { text: '取消', onclick: finish }),
      h('button', {
        class: 'primary',
        text: '开始同步',
        onclick: async (ev) => {
          const ids = boxes.filter((b) => b.checked).map((b) => Number(b.value));
          if (!ids.length) {
            toast('请至少选择一个存储源');
            return;
          }
          ev.target.disabled = true;
          try {
            await call('/api/file/sync', { fileId: file.id, providerIds: ids });
            finish();
            toast('已加入同步队列', 'ok');
            navigate(`/detail/${encodeURIComponent(file.id)}`);
          } catch (err) {
            toast(`同步失败：${err.message}`, 'err');
            ev.target.disabled = false;
          }
        },
      }),
    ],
    onClose: finish,
  });
}
