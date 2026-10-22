// 回收站：恢复（回原目录）/ 彻底删除 / 清空。
//
// 与客户端的差别：这里不做"移动到…再恢复"，只提供原位恢复——跨目录恢复在
// 大目录树上容易点错，客户端也没把它放在首屏。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, setChildren } from '../dom.js';
import { fmtSize, fmtTime, iconFor, isDir } from '../format.js';
import { back } from '../router.js';

let renderToken = 0;

export async function renderTrash(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🗑️ 回收站' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }),
    h('button', { class: 'danger', text: '清空回收站', onclick: () => emptyTrash(reload) }));
  const content = h('div');
  setChildren(main, h('div', { class: 'page' }, head, content));

  async function load() {
    setChildren(content, h('div', { class: 'empty' }, h('div', { class: 'spinner', style: { margin: '0 auto' } })));
    try {
      const data = await call('/api/file/trash/list', {});
      if (!alive()) return;
      const files = data.files || [];
      if (!files.length) {
        setChildren(content, emptyState('回收站是空的', '🗑️'));
        return;
      }
      setChildren(content, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
        h('thead', null, h('tr', null,
          h('th', { text: '名称' }),
          h('th', { class: 'size', text: '大小' }),
          h('th', { class: 'time', text: '删除时间' }),
          h('th', { class: 'act' }))),
        h('tbody', null, files.map((f) => row(f, reload))))));
    } catch (err) {
      if (!alive()) return;
      setChildren(content, errorState(`加载失败：${err.message}`, load));
    }
  }

  const reload = () => {
    renderToken += 1;
    // 重新进入本页（保持路由不变）
    renderTrash(main, route);
  };

  await load();
}

function row(f, reload) {
  return h('tr', null,
    h('td', null, h('div', { class: 'name-cell' },
      h('span', { class: 'ico', text: iconFor(f) }),
      h('span', { class: 'label', text: f.name || '(未命名)' }))),
    h('td', { class: 'size', text: isDir(f) ? '-' : fmtSize(f.size) }),
    h('td', { class: 'time', text: fmtTime(f.deletedAt) }),
    h('td', { class: 'act' },
      h('span', { class: 'linklike', text: '恢复', onclick: () => restore(f, reload) }),
      h('span', { class: 'linklike', text: '彻底删除', onclick: () => removeForever(f, reload) })));
}

async function restore(file, reload) {
  try {
    await call('/api/file/trash/restore', { ids: [file.id] });
    toast('已恢复', 'ok');
    reload();
  } catch (err) {
    toast(`恢复失败：${err.message}`, 'err');
  }
}

async function removeForever(file, reload) {
  const ok = await confirmDialog(
    `彻底删除「${file.name}」？文件与所有存储副本都会被清理，无法恢复。`,
    { title: '彻底删除', okText: '彻底删除', danger: true },
  );
  if (!ok) return;
  try {
    await call('/api/file/trash/delete-permanently', { ids: [file.id] });
    toast('已彻底删除', 'ok');
    reload();
  } catch (err) {
    toast(`删除失败：${err.message}`, 'err');
  }
}

async function emptyTrash(reload) {
  const ok = await confirmDialog(
    '清空回收站会彻底删除其中的全部内容，无法恢复。确定继续？',
    { title: '清空回收站', okText: '清空', danger: true },
  );
  if (!ok) return;
  try {
    await call('/api/file/trash/empty', {});
    toast('回收站已清空', 'ok');
    reload();
  } catch (err) {
    toast(`清空失败：${err.message}`, 'err');
  }
}
