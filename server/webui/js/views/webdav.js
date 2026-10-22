// WebDAV 账号（每个用户管理自己的账号，不是管理员功能）。
//
// 账号 = 独立用户名 + 密码 + 绑定到网盘里某个目录，用任意 WebDAV 客户端挂载后
// 就能像本地盘一样读写。密码是哈希存储的，**只在创建 / 重置的那一次响应里返回明文**，
// 所以那一步必须让用户自己复制走，关掉就再也看不到（只能重置）。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, openModal, setChildren, copyText } from '../dom.js';
import { fmtTime } from '../format.js';
import { pickFolder } from '../components/picker.js';
import { back } from '../router.js';
import { store } from '../store.js';

let renderToken = 0;

export async function renderWebdav(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';

  const body = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🔌 WebDAV 账号' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, body));

  setChildren(body, h('div', { class: 'muted small', text: '加载中…' }));

  let accounts = [];
  let davUrl = '';
  try {
    const data = await call('/api/webdav/accounts/list', {});
    if (!alive()) return;
    accounts = data.accounts || [];
    davUrl = data.davUrl || '/dav';
  } catch (err) {
    if (!alive()) return;
    setChildren(body, errorState(`加载失败：${err.message}`, () => renderWebdav(main, route)));
    return;
  }

  const fullDavUrl = `${location.origin}${davUrl}`;
  const addrBox = h('div', { class: 'muted small', style: { marginBottom: '12px' } },
    h('span', { text: '服务地址：' }),
    h('code', { text: fullDavUrl }),
    h('span', {
      class: 'linklike',
      text: '复制',
      onclick: async () => {
        const ok = await copyText(fullDavUrl);
        toast(ok ? '地址已复制' : '复制失败，请手动选中复制', ok ? 'ok' : 'err');
      },
    }));

  const tableWrap = h('div');
  setChildren(body,
    addrBox,
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', text: '＋ 新建账号', onclick: () => createDialog() }),
      h('button', { text: '刷新', onclick: () => renderWebdav(main, route) }),
      h('div', { class: 'spacer' }),
      h('span', { class: 'muted small', text: `共 ${accounts.length} 个账号` })),
    tableWrap,
    h('div', { class: 'muted small', style: { marginTop: '10px' },
      text: '用支持 WebDAV 的客户端（RaiDrive、Windows 映射网络驱动器、macOS 访达…）填上面的地址与账号密码即可挂载。' }));

  function renderTable() {
    if (!alive()) return;
    if (!accounts.length) {
      setChildren(tableWrap, emptyState('还没有 WebDAV 账号', '🔌'));
      return;
    }
    setChildren(tableWrap, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { text: '用户名 / 标签' }),
        h('th', { text: '绑定目录' }),
        h('th', { text: '状态' }),
        h('th', { text: '最后使用' }),
        h('th', { class: 'act' }))),
      h('tbody', null, accounts.map(row)))));
  }

  function row(acc) {
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '🔌' }),
          h('span', { class: 'label', text: acc.username || `#${acc.id}` }),
          acc.readOnly ? h('span', { class: 'status-chip', text: '只读' }) : null),
        h('div', { class: 'muted small', text: acc.name || '—' })),
      h('td', null,
        h('div', { class: 'muted small', text: acc.rootPath || '-' }),
        acc.rootValid === false
          ? h('div', { class: 'error-text small', text: '绑定目录已失效（已删除或进回收站）' })
          : null),
      h('td', null,
        h('span', { class: `status-chip ${acc.enabled ? 'ok' : 'warn'}`.trim(), text: acc.enabled ? '启用' : '停用' }),
        acc.disabledReason
          ? h('div', { class: 'muted small', text: acc.disabledReason })
          : null),
      h('td', { class: 'time', text: acc.lastUsedTime ? fmtTime(acc.lastUsedTime) : '从未使用' }),
      h('td', { class: 'act' },
        h('span', { class: 'linklike', text: '重置密码', onclick: () => resetPassword(acc) }),
        h('span', { class: 'linklike', text: '编辑', onclick: () => editDialog(acc) }),
        h('span', { class: 'linklike danger', text: '删除', onclick: () => remove(acc) })));
  }

  // 明文密码只出现这一次：单独弹窗 + 复制按钮
  function showSecret(username, password) {
    const modal = openModal({
      title: '账号密码（只显示这一次）',
      body: h('div', null,
        h('div', { class: 'field' }, h('label', { text: '用户名' }), h('code', { text: username })),
        h('div', { class: 'field' }, h('label', { text: '密码' }), h('code', { text: password })),
        h('div', { class: 'muted small', text: '关掉这个窗口就看不到密码了（服务端只存哈希），需要时只能重置。' })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', {
          text: '复制账号密码',
          onclick: async () => {
            const ok = await copyText(`用户名：${username}\n密码：${password}`);
            toast(ok ? '已复制' : '复制失败，请手动选中', ok ? 'ok' : 'err');
          },
        }),
        h('button', { class: 'primary', text: '我已保存', onclick: () => modal.close() })),
    });
  }

  function createDialog() {
    const nameInput = h('input', { type: 'text', placeholder: '可选，如 我的笔记本' });
    const userInput = h('input', { type: 'text', placeholder: '可选，留空则由服务端生成' });
    const passInput = h('input', { type: 'text', placeholder: '可选，留空则由服务端生成随机密码' });
    const readOnly = h('input', { type: 'checkbox' });
    const target = h('span', { class: 'muted small', text: '根目录' });
    let targetId = store.rootId || '';

    const modal = openModal({
      title: '新建 WebDAV 账号',
      body: h('div', null,
        field('标签', nameInput),
        h('div', { class: 'field' },
          h('label', { text: '绑定目录' }),
          h('div', { class: 'toolbar', style: { marginBottom: '0' } },
            target,
            h('button', {
              class: 'sm',
              text: '选择目录',
              onclick: async () => {
                const picked = await pickFolder({ title: '绑定目录', okText: '选这个目录' });
                if (!picked) return;
                targetId = picked.id;
                target.textContent = picked.id === store.rootId ? '根目录' : (picked.name || '已选目录');
              },
            })),
          h('div', { class: 'muted small', text: '客户端挂载后只能看到这个目录及其子目录。' })),
        field('用户名', userInput),
        field('密码', passInput),
        h('div', { class: 'field' }, h('label', null, readOnly, h('span', { text: ' 只读（允许下载，禁止写入）' })))),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '创建',
          onclick: async () => {
            if (!targetId) {
              toast('请选择绑定目录');
              return;
            }
            const req = { rootFileId: targetId, readOnly: readOnly.checked };
            if (nameInput.value.trim()) req.name = nameInput.value.trim();
            if (userInput.value.trim()) req.username = userInput.value.trim();
            if (passInput.value.trim()) req.password = passInput.value.trim();
            try {
              const data = await call('/api/webdav/accounts/create', req);
              modal.close();
              await renderWebdav(main, route);
              showSecret(data.account?.username || req.username || '', data.password || req.password || '');
            } catch (err) {
              toast(`创建失败：${err.rawMessage || err.message}`, 'err');
            }
          },
        })),
    });
  }

  function editDialog(acc) {
    const nameInput = h('input', { type: 'text', value: acc.name || '' });
    const enabled = h('input', { type: 'checkbox' });
    enabled.checked = Boolean(acc.enabled);
    const readOnly = h('input', { type: 'checkbox' });
    readOnly.checked = Boolean(acc.readOnly);
    const target = h('span', { class: 'muted small', text: acc.rootPath || '未绑定' });
    let targetId = '';

    const modal = openModal({
      title: `编辑账号 · ${acc.username}`,
      body: h('div', null,
        field('标签', nameInput),
        h('div', { class: 'field' },
          h('label', { text: '绑定目录' }),
          h('div', { class: 'toolbar', style: { marginBottom: '0' } },
            target,
            h('button', {
              class: 'sm',
              text: '选择目录',
              onclick: async () => {
                const picked = await pickFolder({ title: '绑定目录', okText: '选这个目录' });
                if (!picked) return;
                targetId = picked.id;
                target.textContent = picked.id === store.rootId ? '根目录' : (picked.name || '已选目录');
              },
            }))),
        h('div', { class: 'field' }, h('label', null, readOnly, h('span', { text: ' 只读' }))),
        h('div', { class: 'field' }, h('label', null, enabled, h('span', { text: ' 启用' })))),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '保存',
          onclick: async () => {
            const req = { id: Number(acc.id), readOnly: readOnly.checked, enabled: enabled.checked };
            req.name = nameInput.value.trim();
            if (targetId) req.rootFileId = targetId;
            try {
              await call('/api/webdav/accounts/update', req);
              toast('已保存', 'ok');
              modal.close();
              await renderWebdav(main, route);
            } catch (err) {
              toast(`保存失败：${err.rawMessage || err.message}`, 'err');
            }
          },
        })),
    });
  }

  async function resetPassword(acc) {
    const input = h('input', { type: 'text', placeholder: '留空 = 服务端生成随机密码' });
    const modal = openModal({
      title: `重置密码 · ${acc.username}`,
      body: h('div', null,
        h('div', { class: 'field' }, h('label', { text: '新密码' }), input),
        h('div', { class: 'muted small', text: '重置后旧密码立即失效，需要到客户端里改。' })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '重置',
          onclick: async () => {
            const req = { id: Number(acc.id) };
            if (input.value.trim()) req.password = input.value.trim();
            try {
              const data = await call('/api/webdav/accounts/reset-password', req);
              modal.close();
              showSecret(acc.username, data.password || req.password || '');
            } catch (err) {
              toast(`重置失败：${err.rawMessage || err.message}`, 'err');
            }
          },
        })),
    });
  }

  async function remove(acc) {
    const ok = await confirmDialog(
      `删除 WebDAV 账号「${acc.username}」？\n挂载着的客户端会立即失去访问权限，网盘里的文件不受影响。`,
      { title: '删除账号', okText: '删除', danger: true });
    if (!ok) return;
    try {
      await call('/api/webdav/accounts/delete', { id: Number(acc.id) });
      toast('已删除', 'ok');
      await renderWebdav(main, route);
    } catch (err) {
      toast(`删除失败：${err.rawMessage || err.message}`, 'err');
    }
  }

  function field(label, input) {
    return h('div', { class: 'field' }, h('label', { text: label }), input);
  }

  renderTable();
}
