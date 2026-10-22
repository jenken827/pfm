// 管理 → 用户与配额（仅管理员）。
//
// 数据来自三处，拼在一起看：
//   /api/admin/user/list    用户（display_name / email / roles）
//   /api/admin/user/spaces  用户空间（根目录 / 配额 / 已用 / 绑定存储源）
//   /api/admin/provider/list 存储源（把 id 换成名字）
// 没有空间的用户也要列出来——那正是"还没分配根目录"的人，需要在这里分配。
//
// 两个写操作都只影响配置、不搬数据：
//   update-space  只改配额（0 = 不限）
//   assign-root   已存在空间的用户只改 root 目录绑定（既有文件仍留在原 provider 上）

import { call } from '../api.js';
import { h, toast, emptyState, errorState, openModal, setChildren } from '../dom.js';
import { fmtSize, fmtSize0, fmtTime, shortHash } from '../format.js';
import { parseProviderIds } from '../components/provider_ids.js';
import { back } from '../router.js';
import { session } from '../session.js';

const GB = 1024 * 1024 * 1024;

let renderToken = 0;

export async function renderAdminUsers(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';

  const body = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '👥 用户与配额' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, body));

  // 非管理员直接给提示：接口本身会 403，这里只是别让人对着空白猜
  if (!session.user?.isAdmin) {
    setChildren(body, emptyState('这个页面只有管理员能看', '🔒'));
    return;
  }

  setChildren(body, h('div', { class: 'muted small', text: '加载中…' }));

  let users = [];
  let spaces = [];
  let providers = [];

  try {
    const [listData, spaceData, providerData] = await Promise.all([
      call('/api/admin/user/list', {}),
      call('/api/admin/user/spaces', { page: 1, pageSize: 200 }),
      call('/api/admin/provider/list', {}),
    ]);
    if (!alive()) return;
    users = listData.users || [];
    spaces = spaceData.spaces || [];
    providers = providerData.providers || [];
  } catch (err) {
    if (!alive()) return;
    setChildren(body, errorState(`加载失败：${err.message}`, () => renderAdminUsers(main, route)));
    return;
  }

  const spaceByUser = new Map(spaces.map((s) => [s.userId, s]));
  const providerName = (id) => providers.find((p) => Number(p.id) === Number(id))?.name || `#${id}`;
  const boundNames = (raw) => {
    const ids = parseProviderIds(raw);
    return ids.length ? ids.map(providerName).join('、') : '未绑定';
  };

  // 用户多的时候按名字 / 邮箱过滤（纯前端过滤，列表本身一次拉全）
  const filter = h('input', { type: 'search', placeholder: '按用户名 / 邮箱过滤…', style: { maxWidth: '320px' } });
  const tableWrap = h('div');

  filter.addEventListener('input', () => renderTable(filter.value.trim().toLowerCase()));

  setChildren(body,
    h('div', { class: 'toolbar' }, filter, h('div', { class: 'spacer' }),
      h('span', { class: 'muted small', text: `${users.length} 个用户 / ${spaces.length} 个已分配空间` })),
    tableWrap);

  function renderTable(keyword) {
    if (!alive()) return;
    const rows = users.filter((u) => !keyword
      || (u.displayName || '').toLowerCase().includes(keyword)
      || (u.email || '').toLowerCase().includes(keyword)
      || (u.userId || '').toLowerCase().includes(keyword));
    if (!rows.length) {
      setChildren(tableWrap, emptyState(keyword ? '没有匹配的用户' : '还没有用户', '👥'));
      return;
    }
    setChildren(tableWrap, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { text: '用户' }),
        h('th', { text: '已用 / 配额' }),
        h('th', { text: '绑定存储源' }),
        h('th', { text: '根目录' }),
        h('th', { class: 'act' }))),
      h('tbody', null, rows.map(row)))));
  }

  function row(user) {
    const space = spaceByUser.get(user.userId);
    const quota = Number(space?.quotaBytes || 0);
    const used = Number(space?.usedBytes || 0);
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '👤' }),
          h('span', { class: 'label', text: user.displayName || user.userId }),
          (user.roles || []).includes('pfm_admin') ? h('span', { class: 'status-chip ok', text: '管理员' }) : null),
        h('div', { class: 'muted small', text: user.email || '—' })),
      h('td', null,
        h('div', { text: quota > 0 ? `${fmtSize0(used)} / ${fmtSize(quota)}` : `${fmtSize0(used)} / 不限` }),
        quota > 0 && used >= quota
          ? h('div', { class: 'error-text small', text: '已超出配额' })
          : null),
      h('td', { class: 'muted small', text: space ? boundNames(space.boundProviderIds) : '未分配' }),
      h('td', { class: 'muted small', text: space ? shortHash(space.rootFileId, 8) : '—' }),
      h('td', { class: 'act' },
        space ? h('span', { class: 'linklike', text: '配额', onclick: () => editQuota(user, space) }) : null,
        h('span', { class: 'linklike', text: space ? '改绑定' : '分配根目录', onclick: () => assignRoot(user, space) })));
  }

  async function editQuota(user, space) {
    const quota = Number(space.quotaBytes || 0);
    const input = h('input', {
      type: 'number',
      min: '0',
      step: '0.1',
      value: quota > 0 ? (quota / GB).toFixed(2) : '',
      placeholder: '留空或 0 表示不限',
    });
    const modal = openModal({
      title: `调整配额 · ${user.displayName || user.userId}`,
      body: h('div', null,
        h('div', { class: 'field' }, h('label', { text: '配额（GB）' }), input),
        h('div', { class: 'muted small', text: '填 0 或留空 = 不限制；当前已用 ' + fmtSize0(space.usedBytes) })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', { class: 'primary', text: '保存', onclick: () => saveQuota(user, input.value, modal) })),
    });
    input.focus();
  }

  async function saveQuota(user, raw, modal) {
    const text = String(raw ?? '').trim();
    const gb = text === '' ? 0 : Number(text);
    if (!Number.isFinite(gb) || gb < 0) {
      toast('配额要填不小于 0 的数字（GB）');
      return;
    }
    const bytes = Math.round(gb * GB);
    try {
      await call('/api/admin/user/update-space', { userId: user.userId, quotaBytes: String(bytes) });
      toast(bytes > 0 ? `已设为 ${fmtSize(bytes)}` : '已取消配额限制', 'ok');
      modal.close();
      await renderAdminUsers(main, route);
    } catch (err) {
      toast(`保存失败：${err.message}`, 'err');
    }
  }

  function assignRoot(user, space) {
    if (!providers.length) {
      toast('还没有存储源，先去「存储源」添加', 'err');
      return;
    }
    const current = new Set(parseProviderIds(space?.boundProviderIds));
    const boxes = providers.map((p) => {
      const input = h('input', { type: 'checkbox' });
      input.checked = current.has(Number(p.id));
      input.disabled = !p.enabled;
      return { id: Number(p.id), name: p.name, input };
    });
    const list = h('div', { style: { display: 'grid', gap: '8px' } }, boxes.map((b) =>
      h('label', { style: { display: 'flex', gap: '8px', alignItems: 'center', cursor: 'pointer' } },
        b.input,
        h('span', { text: b.name || `#${b.id}` }),
        b.input.disabled ? h('span', { class: 'status-chip warn', text: '已停用' }) : null)));
    const modal = openModal({
      title: space ? `改根目录绑定 · ${user.displayName || user.userId}` : `分配根目录 · ${user.displayName || user.userId}`,
      body: h('div', null,
        h('div', { class: 'field' }, h('label', { text: '用户可用的存储源' }), list),
        h('div', { class: 'muted small', text: space
          ? '只改根目录的绑定关系：已存在的文件仍留在原存储源，之后的新上传落到新选择里。'
          : '会为该用户创建根目录，并把这些存储源作为其可用范围（子目录绑定不能超出这个范围）。' })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: space ? '保存绑定' : '创建根目录',
          onclick: async () => {
            const ids = boxes.filter((b) => b.input.checked).map((b) => b.id);
            if (!ids.length) {
              toast('至少选一个存储源');
              return;
            }
            // 不再叠一层确认框：弹窗里已经写清了影响，按钮文案（创建根目录 / 保存绑定）
            // 本身就是确认动作；两层模态互相遮挡反而让人不知道该点哪个。
            try {
              await call('/api/admin/user/assign-root', { userId: user.userId, providerIds: ids });
              toast(space ? '绑定已更新' : '根目录已创建', 'ok');
              modal.close();
              await renderAdminUsers(main, route);
            } catch (err) {
              toast(`操作失败：${err.message}`, 'err');
            }
          },
        })),
    });
  }

  renderTable('');

  // 页脚备注创建时间，方便对照客户端"关于"里的信息
  if (users.length) {
    const latest = users.reduce((acc, u) => (!acc || String(u.createdTime || '') > acc ? String(u.createdTime || '') : acc), '');
    if (latest) body.appendChild(h('div', { class: 'muted small', style: { marginTop: '10px' }, text: `最近注册：${fmtTime(latest)}` }));
  }
}
