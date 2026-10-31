// MCP 令牌（每个用户管理自己的令牌，不是管理员功能）。
//
// 令牌给长期运行的 AI 客户端用：可以限定"只读 / 可写"和"只能看见某个目录"，
// 明文只在创建的那一次响应里出现（服务端只存 sha256），关掉窗口就再也看不到。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, openModal, setChildren, copyText } from '../dom.js';
import { fmtTime } from '../format.js';
import { pickFolder } from '../components/picker.js';
import { back } from '../router.js';
import { store } from '../store.js';

let renderToken = 0;

export async function renderMcp(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';

  const body = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🤖 MCP 令牌' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, body));
  setChildren(body, h('div', { class: 'muted small', text: '加载中…' }));

  let tokens = [];
  try {
    const data = await call('/api/mcp/token/list', {});
    if (!alive()) return;
    tokens = data.tokens || [];
  } catch (err) {
    if (!alive()) return;
    setChildren(body, errorState(`加载失败：${err.message}`, () => renderMcp(main, route)));
    return;
  }

  const endpoint = `${location.origin}/mcp`;
  const tableWrap = h('div');
  setChildren(body,
    h('div', { class: 'muted small', style: { marginBottom: '12px' } },
      h('span', { text: 'MCP 端点：' }),
      h('code', { text: endpoint }),
      h('span', {
        class: 'linklike',
        text: '复制',
        onclick: async () => {
          const ok = await copyText(endpoint);
          toast(ok ? '地址已复制' : '复制失败，请手动选中复制', ok ? 'ok' : 'err');
        },
      })),
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', text: '＋ 新建令牌', onclick: () => createDialog() }),
      h('button', { text: '刷新', onclick: () => renderMcp(main, route) }),
      h('div', { class: 'spacer' }),
      h('span', { class: 'muted small', text: `共 ${tokens.length} 个令牌` })),
    tableWrap,
    h('div', { class: 'muted small', style: { marginTop: '10px' },
      text: '令牌只对 MCP 端点生效，不能拿去调其它接口；撤销后立刻失效。服务端关闭 MCP（mcp.enabled）时这些令牌都用不了。' }));

  function renderTable() {
    if (!alive()) return;
    if (!tokens.length) {
      setChildren(tableWrap, emptyState('还没有 MCP 令牌', '🤖'));
      return;
    }
    setChildren(tableWrap, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { text: '标签' }),
        h('th', { text: '权限' }),
        h('th', { text: '可见范围' }),
        h('th', { text: '状态' }),
        h('th', { text: '最后使用' }),
        h('th', { class: 'act' }))),
      h('tbody', null, tokens.map(row)))));
  }

  function row(item) {
    const scope = item.rootFolderId ? (item.rootFolderPath || '已删除的目录') : '整棵树';
    let statusText = '有效';
    let statusClass = 'ok';
    if (item.expired) {
      statusText = '已过期';
      statusClass = 'warn';
    }
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '🤖' }),
          h('span', { class: 'label', text: item.name || `#${item.id}` })),
        h('div', { class: 'muted small', text: item.ownerName ? `归属 ${item.ownerName}` : `#${item.id}` })),
      h('td', null,
        h('span', { class: 'status-chip ok', text: item.allowRead ? '只读' : '无读权限' }),
        item.allowWrite ? h('span', { class: 'status-chip warn', text: '可写' }) : null),
      h('td', null, h('div', { class: 'muted small', text: scope })),
      h('td', null,
        h('span', { class: `status-chip ${statusClass}`.trim(), text: statusText }),
        item.expiresAt ? h('div', { class: 'muted small', text: `到期 ${fmtTime(item.expiresAt)}` }) : null),
      h('td', { class: 'time', text: item.lastUsedAt ? fmtTime(item.lastUsedAt) : '从未使用' }),
      h('td', { class: 'act' },
        h('span', { class: 'linklike danger', text: '撤销', onclick: () => revoke(item) })));
  }

  // 明文只出现这一次：连客户端配置一起给出来，省得用户自己拼 JSON
  function showSecret(name, plaintext) {
    const snippet = JSON.stringify({
      mcpServers: { pfm: { url: endpoint, headers: { Authorization: `Bearer ${plaintext}` } } },
    }, null, 2);
    const modal = openModal({
      title: 'MCP 令牌（只显示这一次）',
      body: h('div', null,
        h('div', { class: 'field' }, h('label', { text: '标签' }), h('code', { text: name })),
        h('div', { class: 'field' }, h('label', { text: '令牌' }), h('code', { text: plaintext })),
        h('div', { class: 'field' }, h('label', { text: '客户端配置' }), h('pre', { text: snippet })),
        h('div', { class: 'muted small', text: '关掉这个窗口就看不到令牌了（服务端只存哈希），需要时只能重新创建。' })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', {
          text: '复制配置',
          onclick: async () => {
            const ok = await copyText(snippet);
            toast(ok ? '已复制' : '复制失败，请手动选中', ok ? 'ok' : 'err');
          },
        }),
        h('button', {
          text: '复制令牌',
          onclick: async () => {
            const ok = await copyText(plaintext);
            toast(ok ? '已复制' : '复制失败，请手动选中', ok ? 'ok' : 'err');
          },
        }),
        h('button', { class: 'primary', text: '我已保存', onclick: () => modal.close() })),
    });
  }

  function createDialog() {
    const nameInput = h('input', { type: 'text', placeholder: '比如 笔记本 / Cursor' });
    const allowWrite = h('input', { type: 'checkbox' });
    const daysInput = h('input', { type: 'number', value: '90', min: '0', max: '3650' });
    const target = h('span', { class: 'muted small', text: '整棵树' });
    let targetId = '';

    const modal = openModal({
      title: '新建 MCP 令牌',
      body: h('div', null,
        field('标签（便于日后认出）', nameInput),
        field('有效期（天，0 = 不过期）', daysInput),
        h('div', { class: 'field' },
          h('label', { text: '可见范围' }),
          h('div', { class: 'toolbar', style: { marginBottom: '0' } },
            target,
            h('button', {
              class: 'sm',
              text: '选择目录',
              onclick: async () => {
                const picked = await pickFolder({ title: '可见范围', okText: '只看这个目录' });
                if (!picked) return;
                targetId = picked.id;
                target.textContent = picked.id === store.rootId ? '整棵树' : (picked.name || '已选目录');
              },
            }),
            h('button', {
              class: 'sm',
              text: '整棵树',
              onclick: () => {
                targetId = '';
                target.textContent = '整棵树';
              },
            })),
          h('div', { class: 'muted small', text: '限定目录后，客户端看到的 / 就是那个目录，出不去。' })),
        h('div', { class: 'field' }, h('label', null, allowWrite, h('span', { text: ' 允许写（建目录 / 写入 / 移动 / 删除进回收站）' })),
          h('div', { class: 'muted small', text: '还需要服务端配置 mcp.allow_write 打开，否则写工具不会出现。' }))),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '创建',
          onclick: async () => {
            const name = nameInput.value.trim();
            if (!name) {
              toast('请填一个标签');
              return;
            }
            const req = {
              name,
              allowWrite: allowWrite.checked,
              expiresInDays: Number(daysInput.value) > 0 ? Number(daysInput.value) : 0,
            };
            if (targetId) req.rootFolderId = targetId;
            try {
              const data = await call('/api/mcp/token/create', req);
              modal.close();
              await renderMcp(main, route);
              showSecret(data.token?.name || name, data.plaintext || '');
            } catch (err) {
              toast(`创建失败：${err.rawMessage || err.message}`, 'err');
            }
          },
        })),
    });
  }

  async function revoke(item) {
    const ok = await confirmDialog(
      `撤销令牌「${item.name || `#${item.id}`}」？\n用它的客户端会立即失去访问权限，网盘里的文件不受影响。`,
      { title: '撤销令牌', okText: '撤销', danger: true });
    if (!ok) return;
    try {
      await call('/api/mcp/token/revoke', { id: Number(item.id) });
      toast('已撤销', 'ok');
      await renderMcp(main, route);
    } catch (err) {
      toast(`撤销失败：${err.rawMessage || err.message}`, 'err');
    }
  }

  function field(label, input) {
    return h('div', { class: 'field' }, h('label', { text: label }), input);
  }

  renderTable();
}
