// 离线下载：服务端通过 aria2 拉取 URL 再落盘到指定目录。
//
// 浏览器不能直接让服务端下载任意 URL 之外的事情，所以这里只做"提交任务 + 看状态"，
// 进度刷新靠手动/定时拉列表（aria2 的进度在下载记录里不是实时推送的）。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, setChildren } from '../dom.js';
import { fmtSize, fmtTime, downloadState } from '../format.js';
import { back } from '../router.js';
import { ensureSpace, store } from '../store.js';
import { pickFolder } from '../components/picker.js';

let renderToken = 0;
const POLL_MS = 4000;

export async function renderOffline(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';
  await ensureSpace().catch(() => {});

  let targetId = store.rootId;
  const targetLabel = h('span', { class: 'muted small', text: '根目录' });
  const urlInput = h('input', { type: 'url', placeholder: 'https://example.com/file.zip' });
  const nameInput = h('input', { type: 'text', placeholder: '留空则用远端文件名' });

  const form = h('div', { class: 'card', style: { padding: '14px' } },
    h('div', { class: 'field' }, h('label', { text: '下载地址' }), urlInput),
    h('div', { class: 'field' }, h('label', { text: '保存文件名' }), nameInput),
    h('div', { class: 'field' },
      h('label', { text: '保存到' }),
      h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        targetLabel,
        h('button', {
          class: 'sm',
          text: '选择目录',
          onclick: async () => {
            const id = await pickFolder({ title: '保存到', okText: '选这个目录' });
            if (!id) return;
            targetId = id;
            targetLabel.textContent = id === store.rootId ? '根目录' : `目录 ${id.slice(0, 8)}…`;
          },
        }))),
    h('button', {
      class: 'primary',
      text: '开始离线下载',
      onclick: async (ev) => {
        const url = urlInput.value.trim();
        if (!url) {
          toast('请填写下载地址');
          return;
        }
        ev.target.disabled = true;
        try {
          const body = { url, parentId: targetId };
          if (nameInput.value.trim()) body.fileName = nameInput.value.trim();
          await call('/api/download/offline/create', body);
          toast('已提交离线下载', 'ok');
          urlInput.value = '';
          nameInput.value = '';
          await load();
        } catch (err) {
          toast(`提交失败：${err.message}`, 'err');
        } finally {
          ev.target.disabled = false;
        }
      },
    }));

  const content = h('div', { style: { marginTop: '14px' } });
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '⬇️ 离线下载' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, form, content));

  let page = 1;
  const pageSize = 50;
  let timer = null;

  async function load() {
    if (!alive()) return;
    clearTimeout(timer);
    try {
      const data = await call('/api/download/offline/list', { page, pageSize });
      if (!alive()) return;
      const records = data.records || [];
      render(records, data.total || records.length);
      // 有进行中的任务才继续轮询，避免空转
      const pending = records.some((r) => r.state === 'DOWNLOAD_PENDING' || r.state === 'DOWNLOADING');
      if (pending) timer = setTimeout(load, POLL_MS);
    } catch (err) {
      if (!alive()) return;
      setChildren(content, errorState(`加载失败：${err.message}`, load));
    }
  }

  function render(records, total) {
    if (!records.length) {
      setChildren(content, emptyState('还没有离线下载任务', '⬇️'));
      return;
    }
    setChildren(content, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { text: '名称 / 来源' }),
        h('th', { class: 'size', text: '大小' }),
        h('th', { text: '状态' }),
        h('th', { class: 'time', text: '创建时间' }),
        h('th', { class: 'act' }))),
      h('tbody', null, records.map(row)))),
    h('div', { class: 'muted small', style: { marginTop: '10px' }, text: `共 ${total} 条` }));
  }

  function row(rec) {
    const [text, tone] = downloadState(rec.state);
    const name = (rec.filePath || '').split('/').pop() || rec.sourceName || rec.url || '(未知)';
    return h('tr', null,
      h('td', null, h('div', { class: 'name-cell' },
        h('span', { class: 'ico', text: '⬇️' }),
        h('span', { class: 'label', text: name, title: rec.url || '' }),
        rec.errorMessage ? h('span', { class: 'badge warn', text: rec.errorMessage }) : null)),
      h('td', { class: 'size', text: fmtSize(rec.fileSize) }),
      h('td', null, h('span', { class: `status-chip ${tone}`.trim(), text })),
      h('td', { class: 'time', text: fmtTime(rec.createdAt) }),
      h('td', { class: 'act' },
        rec.state === 'DOWNLOAD_FAILED'
          ? h('span', { class: 'linklike', text: '重试', onclick: () => retry(rec) }) : null,
        h('span', { class: 'linklike', text: '删除', onclick: () => remove(rec) })));
  }

  async function retry(rec) {
    try {
      await call('/api/download/offline/retry', { id: Number(rec.id) });
      toast('已重试', 'ok');
      await load();
    } catch (err) {
      toast(`重试失败：${err.message}`, 'err');
    }
  }

  async function remove(rec) {
    const ok = await confirmDialog('删除这条离线下载记录？（已下载的文件不会被删除）', {
      title: '删除记录',
      okText: '删除',
      danger: true,
    });
    if (!ok) return;
    try {
      await call('/api/download/offline/delete', { id: Number(rec.id) });
      toast('已删除', 'ok');
      await load();
    } catch (err) {
      toast(`删除失败：${err.message}`, 'err');
    }
  }

  await load();
  window.addEventListener('hashchange', () => clearTimeout(timer), { once: true });
}
