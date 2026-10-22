// 我的分享：列表 / 复制 / 打开 / 取消。
//
// 分享链接是**公开**的（拿到链接即可下载，无需登录），所以这里只在用户点击
// "复制"时把链接写进剪贴板，不在页面里直接渲染成可点的 href——避免误触。
// （"打开"是显式按钮，会用新标签页打开。）

import { call } from '../api.js';
import { h, toast, confirmDialog, copyText, emptyState, errorState, openModal, setChildren } from '../dom.js';
import { fmtSize, fmtRemain, shareStatus, typeText } from '../format.js';
import { back } from '../router.js';

const PAGE_SIZE = 100;
let renderToken = 0;

export async function renderShares(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';
  const content = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🔗 我的分享' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, content));

  let offset = 0;
  let items = [];

  async function load(more = false) {
    try {
      const data = await call('/api/file/share/list', { offset, pageSize: PAGE_SIZE });
      if (!alive()) return;
      items = more ? items.concat(data.items || []) : (data.items || []);
      offset = items.length;
      render(data.hasMore);
    } catch (err) {
      if (!alive()) return;
      setChildren(content, errorState(`加载失败：${err.message}`, () => load(false)));
    }
  }

  function render(hasMore) {
    if (!items.length) {
      setChildren(content, emptyState('还没有创建过分享链接', '🔗'));
      return;
    }
    const body = h('tbody', null, items.map((it) => row(it)));
    setChildren(content, 
      h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
        h('thead', null, h('tr', null,
          h('th', { text: '文件' }),
          h('th', { class: 'size', text: '大小' }),
          h('th', { text: '状态' }),
          h('th', { class: 'time', text: '有效期' }),
          h('th', { class: 'time', text: '访问' }),
          h('th', { class: 'act' }))),
        body)),
      hasMore ? h('div', { style: { textAlign: 'center', marginTop: '14px' } },
        h('button', { text: '加载更多', onclick: () => load(true) })) : null,
    );
  }

  function row(item) {
    const [statusText, tone] = shareStatus(item.status);
    const missing = item.fileMissing || item.fileInTrash;
    return h('tr', null,
      h('td', null, h('div', { class: 'name-cell' },
        h('span', { class: 'ico', text: missing ? '⚠️' : '📄' }),
        h('span', { class: 'label', text: item.fileName || item.fileId || '(未知文件)' }),
        missing ? h('span', { class: 'badge warn', text: item.fileInTrash ? '在回收站' : '文件已删除' }) : null)),
      h('td', { class: 'size', text: fmtSize(item.fileSize) }),
      h('td', null, h('span', { class: `status-chip ${tone}`.trim(), text: statusText })),
      h('td', { class: 'time', text: fmtRemain(item.expireAt) }),
      h('td', { class: 'time', text: String(item.accessCount || 0) }),
      h('td', { class: 'act' },
        h('span', { class: 'linklike', text: '打开', onclick: () => window.open(item.shareUrl, '_blank', 'noopener') }),
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
        h('span', { class: 'linklike', text: '编辑', onclick: () => edit(item) }),
        item.status === 'ACTIVE'
          ? h('span', { class: 'linklike', text: '取消', onclick: () => cancel(item) }) : null));
  }

  async function edit(item) {
    const remark = h('input', { type: 'text', value: item.remark || '', placeholder: '备注（可选）' });
    const expire = h('select', null,
      h('option', { value: 'keep', text: '保持不变', selected: true }),
      h('option', { value: '86400', text: '1 天' }),
      h('option', { value: '604800', text: '7 天' }),
      h('option', { value: '2592000', text: '30 天' }),
      h('option', { value: 'never', text: '永久有效' }));
    const maxAccess = h('input', {
      type: 'text',
      value: item.maxAccessCount ? String(item.maxAccessCount) : '',
      placeholder: '留空表示不限',
    });

    let done = false;
    const close = () => {
      if (done) return;
      done = true;
      handle.close();
    };
    const handle = openModal({
      title: `编辑分享 · ${item.fileName || item.shareCode}`,
      body: [
        h('div', { class: 'field' }, h('label', { text: '备注' }), remark),
        h('div', { class: 'field' }, h('label', { text: '有效期' }), expire),
        h('div', { class: 'field' }, h('label', { text: '最多访问次数' }), maxAccess),
      ],
      footer: [
        h('button', { text: '取消', onclick: close }),
        h('button', {
          class: 'primary',
          text: '保存',
          onclick: async (ev) => {
            const body = { shareCode: item.shareCode };
            if (expire.value === 'never') body.neverExpire = true;
            else if (expire.value !== 'keep') body.expireSeconds = Number(expire.value);
            if (maxAccess.value.trim()) body.maxAccessCount = Number(maxAccess.value.trim());
            body.remark = remark.value.trim();
            ev.target.disabled = true;
            try {
              await call('/api/file/share/update', body);
              close();
              toast('已保存', 'ok');
              await load(false);
            } catch (err) {
              toast(`保存失败：${err.message}`, 'err');
              ev.target.disabled = false;
            }
          },
        }),
      ],
      onClose: close,
    });
  }

  async function cancel(item) {
    const ok = await confirmDialog(`取消分享「${item.fileName || item.shareCode}」？链接会立即失效。`, {
      title: '取消分享',
      okText: '取消分享',
      danger: true,
    });
    if (!ok) return;
    try {
      await call('/api/file/share/cancel', { shareCode: item.shareCode });
      toast('已取消', 'ok');
      offset = 0;
      await load(false);
    } catch (err) {
      toast(`取消失败：${err.message}`, 'err');
    }
  }

  await load(false);
}
