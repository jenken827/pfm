// 传输队列：服务端保存的上传任务记录。
//
// 客户端的上传 / 同步 / 目录备份任务都记在这张表里，Web 端自己不上传
// （浏览器算全文哈希成本不划算，见 DESIGN.md §14.11），所以这页只做"看和收拾"：
// 找出卡住 / 失败的任务，重试、删除，或看它落到哪些存储源、卡在哪一步。
// 离线下载是另一套（服务端 aria2 拉取），仍在"离线下载"页，不混进来。
//
// 接口：/api/upload/records/list|detail|retry|retry-all|delete，
// 一律按 owner 过滤——管理员在这页也只能看到自己的任务（服务端就是这么收的）。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, openModal, setChildren } from '../dom.js';
import { fmtSize, fmtTime, locStatus, shortHash, taskType, uploadState } from '../format.js';
import { back, navigate } from '../router.js';
import { loadProviders } from '../store.js';

// 与管理端 / 客户端一致：4 s 一次，只在有进行中的任务时轮询
const POLL_MS = 4000;
const PAGE_SIZE = 20;

// 标签页：[请求里的 status 值, 文案]。空串 = 不过滤
const TABS = [
  ['', '全部'],
  ['PENDING', '等待中'],
  ['UPLOADING', '上传中'],
  ['COMPLETED', '已完成'],
  ['FAILED', '失败'],
];

// 仍在跑的状态：出现这些才需要轮询
const ACTIVE = new Set(['PENDING', 'HASHING', 'UPLOADING']);

let renderToken = 0;

export async function renderTransfers(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  const requested = (route.query.get('status') || '').toUpperCase();
  const status = TABS.some(([value]) => value && value === requested) ? requested : '';

  main.className = 'main';

  // provider id → 名称（targetProviderIds 里存的是 id，直接显示数字没法读）
  const providers = new Map();
  try {
    const list = await loadProviders();
    list.forEach((p) => providers.set(String(p.id), p.name || `#${p.id}`));
  } catch {
    // 存储源拉不到不影响看任务状态，退化成显示 #id
  }
  if (!alive()) return;

  let page = 1;
  let records = [];
  let total = 0;
  let failedCount = 0;
  let timer = null;

  const listWrap = h('div');
  const retryAllBtn = h('button', { text: '重试全部失败', hidden: true, onclick: () => retryAll() });

  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🔁 传输队列' }),
    h('div', { class: 'spacer' }),
    retryAllBtn,
    h('button', { text: '返回文件', onclick: () => back('/files') }));

  const tabs = h('div', { class: 'tabs' }, TABS.map(([value, label]) =>
    h('button', {
      class: value === status ? 'on' : '',
      text: label,
      onclick: () => navigate(`/transfers${value ? `?status=${value}` : ''}`),
    })));

  setChildren(main, h('div', { class: 'page' }, head, tabs, listWrap));

  async function load({ reset = false } = {}) {
    if (!alive()) return;
    clearTimeout(timer);
    if (reset) {
      page = 1;
      records = [];
    }
    try {
      const body = { page, pageSize: PAGE_SIZE };
      if (status) body.status = status;
      const data = await call('/api/upload/records/list', body);
      if (!alive()) return;
      records = records.concat(data.records || []);
      total = Number(data.total || 0);
      failedCount = Number(data.failedCount || 0);
      retryAllBtn.hidden = failedCount === 0;
      retryAllBtn.textContent = `重试全部失败（${failedCount}）`;
      render();
      if (records.some((r) => ACTIVE.has(r.status))) timer = setTimeout(() => load({ reset: true }), POLL_MS);
    } catch (err) {
      if (!alive()) return;
      setChildren(listWrap, errorState(`加载失败：${err.message}`, () => load({ reset: true })));
    }
  }

  function render() {
    if (!records.length) {
      setChildren(listWrap, emptyState(status === 'FAILED' ? '没有失败的任务' : '还没有传输记录', '🔁'));
      return;
    }
    const more = records.length < total;
    setChildren(listWrap,
      h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
        h('thead', null, h('tr', null,
          h('th', { text: '名称' }),
          h('th', { class: 'size', text: '大小' }),
          h('th', { text: '状态' }),
          h('th', { class: 'time', text: '创建时间' }),
          h('th', { class: 'act' }))),
        h('tbody', null, records.map(row)))),
      h('div', { class: 'toolbar', style: { marginTop: '10px' } },
        h('span', { class: 'muted small', text: `共 ${total} 条，已显示 ${records.length} 条` }),
        h('div', { class: 'spacer' }),
        more
          ? h('button', {
            text: '加载更多',
            onclick: async (ev) => {
              ev.target.disabled = true;
              page += 1;
              try {
                await load();
              } finally {
                if (ev.target) ev.target.disabled = false;
              }
            },
          })
          : null));
  }

  function providerNames(rec) {
    let ids = [];
    try {
      ids = JSON.parse(rec.targetProviderIds || '[]');
    } catch {
      return rec.targetProviderIds || '';
    }
    return ids.map((id) => providers.get(String(id)) || `#${id}`).join('、') || '未指定存储源';
  }

  function row(rec) {
    const [text, tone] = uploadState(rec.status);
    const active = ACTIVE.has(rec.status);
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '🔁' }),
          h('span', { class: 'label', text: rec.fileName || '(未知文件名)', title: rec.fileName || '' })),
        h('div', { class: 'muted small', text: `${taskType(rec.taskType)} · ${providerNames(rec)}` }),
        // 失败原因可能很长（云端报错原文），单独一行并截断，不吃掉文件名
        rec.message
          ? h('div', {
            class: 'error-text small',
            title: rec.message,
            text: rec.message,
            style: {
              maxWidth: '520px', marginTop: '2px',
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
            },
          })
          : null),
      h('td', { class: 'size', text: fmtSize(rec.fileSize) }),
      h('td', null, h('span', { class: `status-chip ${tone}`.trim(), text })),
      h('td', { class: 'time', text: fmtTime(rec.createdAt) }),
      h('td', { class: 'act' },
        h('span', { class: 'linklike', text: '详情', onclick: () => showDetail(rec) }),
        !active && rec.status !== 'COMPLETED'
          ? h('span', { class: 'linklike', text: '重试', onclick: () => retry(rec) }) : null,
        h('span', { class: 'linklike', text: '删除', onclick: () => remove(rec) })));
  }

  async function showDetail(rec) {
    const modal = openModal({ title: '任务详情', body: h('div', { class: 'muted small', text: '加载中…' }), wide: true });
    try {
      const data = await call('/api/upload/records/detail', { id: Number(rec.id) });
      if (!alive()) return;
      const item = data.record || rec;
      const locations = data.locations || [];
      const [text, tone] = uploadState(item.status);
      setChildren(modal.root, h('div', { class: 'modal-head' },
        h('h2', { text: '任务详情' }),
        h('button', { class: 'ghost icon', text: '✕', title: '关闭', onclick: () => modal.close() })),
      h('div', { class: 'modal-body' },
        h('div', { class: 'grid2' },
          field('文件名', item.fileName || '-'),
          field('大小', fmtSize(item.fileSize)),
          field('类型', taskType(item.taskType)),
          field('状态', null, h('span', { class: `status-chip ${tone}`.trim(), text })),
          field('创建时间', fmtTime(item.createdAt)),
          field('更新时间', fmtTime(item.updatedAt)),
          field('文件哈希', shortHash(item.fileHash, 32)),
          field('目标存储源', providerNames(item))),
        item.message ? field('错误信息', item.message) : null,
        h('div', { class: 'nav-title', style: { marginTop: '14px' }, text: `落地位置（${locations.length}）` }),
        locations.length
          ? h('div', { class: 'list-wrap' }, h('table', { class: 'files' },
            h('thead', null, h('tr', null,
              h('th', { text: '存储源' }),
              h('th', { text: '状态' }),
              h('th', { class: 'size', text: '大小' }),
              h('th', { text: '说明' }))),
            h('tbody', null, locations.map((loc) => {
              const [lt, lc] = locStatus(loc.status);
              return h('tr', null,
                h('td', { text: loc.providerName || `#${loc.providerId}` }),
                h('td', null, h('span', { class: `status-chip ${lc}`.trim(), text: lt })),
                h('td', { class: 'size', text: fmtSize(loc.size) }),
                h('td', { class: 'muted small', text: loc.message || (loc.progress ? `${loc.progress}%` : '-') }));
            }))))
          : h('div', { class: 'muted small', text: '还没有落到任何存储源（可能仍在等待上传）' })),
      h('div', { class: 'modal-foot' },
        h('button', { text: '关闭', onclick: () => modal.close() })));
    } catch (err) {
      if (!alive()) return;
      setChildren(modal.root, h('div', { class: 'modal-head' },
        h('h2', { text: '任务详情' }),
        h('button', { class: 'ghost icon', text: '✕', title: '关闭', onclick: () => modal.close() })),
      h('div', { class: 'modal-body' },
        h('div', { class: 'error-text', text: `加载失败：${err.message}` })),
      h('div', { class: 'modal-foot' }, h('button', { text: '关闭', onclick: () => modal.close() })));
    }
  }

  function field(label, value, node) {
    return h('div', { class: 'field' },
      h('label', { text: label }),
      node || h('span', { text: value ?? '-' }));
  }

  async function retry(rec) {
    try {
      await call('/api/upload/records/retry', { id: Number(rec.id) });
      toast('已重新排队', 'ok');
      await load({ reset: true });
    } catch (err) {
      toast(`重试失败：${err.message}`, 'err');
    }
  }

  async function retryAll() {
    const ok = await confirmDialog(`把 ${failedCount} 个失败任务全部重新排队？`, {
      title: '重试全部失败',
      okText: '全部重试',
    });
    if (!ok) return;
    try {
      await call('/api/upload/records/retry-all', {});
      toast('已全部重新排队', 'ok');
      await load({ reset: true });
    } catch (err) {
      toast(`重试失败：${err.message}`, 'err');
    }
  }

  async function remove(rec) {
    const ok = await confirmDialog('删除这条传输记录？（不影响已上传成功的文件）', {
      title: '删除记录',
      okText: '删除',
      danger: true,
    });
    if (!ok) return;
    try {
      await call('/api/upload/records/delete', { id: Number(rec.id) });
      toast('已删除', 'ok');
      await load({ reset: true });
    } catch (err) {
      toast(`删除失败：${err.message}`, 'err');
    }
  }

  await load();
  // 离开本页时停掉轮询。不要在这里动 renderToken：hashchange 是先派发给
  // 本监听器、再派发给路由器，加一会把**新**一页的 token 也一并作废，
  // 新视图的 alive() 恒为 false，页面从此不再更新（表现为切到别的筛选后一片空白）。
  window.addEventListener('hashchange', () => clearTimeout(timer), { once: true });
}
