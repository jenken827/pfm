// 管理 → 备份任务（仅管理员）。
//
// 备份任务是"服务器本地目录 → 网盘目录"的增量同步：源路径是**服务端**文件系统上的路径，
// 不是浏览器所在电脑的路径——这一点必须在表单里写明，否则很容易填错。
// 任务按 owner 隔离（只有创建者能管理），所以目标目录用普通目录选择器挑自己的文件夹。
//
// 接口：/api/admin/backup/task/{list,get,create,update,delete}、/api/admin/backup/{trigger,files}

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, openModal, setChildren } from '../dom.js';
import { fmtSize, fmtSize0, fmtTime, shortHash } from '../format.js';
import { pickFolder } from '../components/picker.js';
import { back } from '../router.js';
import { store } from '../store.js';
import { session } from '../session.js';

const POLL_MS = 4000;
const PAGE_SIZE = 20;

const STRATEGY = {
  BACKUP_STRATEGY_UNSPECIFIED: '定时',
  BACKUP_STRATEGY_SCHEDULED: '定时',
  BACKUP_STRATEGY_WATCH: '监听变更',
  BACKUP_STRATEGY_MANUAL: '手动',
};

const RUN_STATUS = {
  BACKUP_RUN_STATUS_UNSPECIFIED: ['未运行', ''],
  BACKUP_RUN_STATUS_IDLE: ['未运行', ''],
  BACKUP_RUN_STATUS_RUNNING: ['运行中', 'warn'],
  BACKUP_RUN_STATUS_SUCCESS: ['成功', 'ok'],
  BACKUP_RUN_STATUS_FAILED: ['失败', 'bad'],
};

const FILE_STATUS = {
  BACKUP_FILE_STATUS_UNSPECIFIED: ['未知', ''],
  BACKUP_FILE_STATUS_PENDING: ['等待中', ''],
  BACKUP_FILE_STATUS_REGISTERING: ['注册中', 'warn'],
  BACKUP_FILE_STATUS_COMPLETED: ['已完成', 'ok'],
  BACKUP_FILE_STATUS_FAILED: ['失败', 'bad'],
  BACKUP_FILE_STATUS_SKIPPED: ['已跳过', ''],
};

let renderToken = 0;

export async function renderAdminBackup(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';

  const body = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🧩 备份任务' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, body));

  if (!session.user?.isAdmin) {
    setChildren(body, emptyState('这个页面只有管理员能看', '🔒'));
    return;
  }

  setChildren(body, h('div', { class: 'muted small', text: '加载中…' }));

  let tasks = [];
  let page = 1;
  let total = 0;
  let timer = null;

  try {
    const data = await call('/api/admin/backup/task/list', { page: 1, pageSize: PAGE_SIZE });
    if (!alive()) return;
    tasks = data.tasks || [];
    total = Number(data.total || tasks.length);
  } catch (err) {
    if (!alive()) return;
    setChildren(body, errorState(`加载失败：${err.message}`, () => renderAdminBackup(main, route)));
    return;
  }

  const tableWrap = h('div');
  setChildren(body,
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', text: '＋ 新建任务', onclick: () => taskDialog() }),
      h('button', { text: '刷新', onclick: () => reload() }),
      h('div', { class: 'spacer' }),
      h('span', { class: 'muted small', text: `共 ${total} 个任务` })),
    tableWrap,
    h('div', { class: 'muted small', style: { marginTop: '10px' },
      text: '源路径是运行 pfm 的那台服务器上的目录；浏览器无法访问你本机的文件系统。' }));

  renderTable();
  maybePoll();

  function maybePoll() {
    clearTimeout(timer);
    if (tasks.some((t) => t.lastRunStatus === 'BACKUP_RUN_STATUS_RUNNING')) {
      timer = setTimeout(() => reload(), POLL_MS);
    }
  }

  async function reload() {
    if (!alive()) return;
    try {
      const data = await call('/api/admin/backup/task/list', { page, pageSize: PAGE_SIZE });
      if (!alive()) return;
      tasks = data.tasks || [];
      total = Number(data.total || tasks.length);
      renderTable();
      maybePoll();
    } catch (err) {
      if (!alive()) return;
      toast(`刷新失败：${err.message}`, 'err');
    }
  }

  function renderTable() {
    if (!alive()) return;
    if (!tasks.length) {
      setChildren(tableWrap, emptyState('还没有备份任务', '🧩'));
      return;
    }
    setChildren(tableWrap,
      h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
        h('thead', null, h('tr', null,
          h('th', { text: '任务' }),
          h('th', { text: '源路径' }),
          h('th', { text: '上次执行' }),
          h('th', { text: '已备份' }),
          h('th', { class: 'act' }))),
        h('tbody', null, tasks.map(row)))),
      total > tasks.length
        ? h('div', { class: 'toolbar', style: { marginTop: '10px' } },
          h('span', { class: 'muted small', text: `共 ${total} 个，已显示 ${tasks.length} 个` }),
          h('div', { class: 'spacer' }),
          h('button', {
            text: '加载更多',
            onclick: async () => {
              page += 1;
              await reload();
            },
          }))
        : null);
  }

  function row(task) {
    const [runText, runTone] = RUN_STATUS[task.lastRunStatus] || ['未运行', ''];
    const strategy = STRATEGY[task.strategy] || '定时';
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '🧩' }),
          h('span', { class: 'label', text: task.name || `#${task.id}`, title: task.name || '' }),
          task.enabled ? null : h('span', { class: 'status-chip warn', text: '已停用' })),
        h('div', { class: 'muted small',
          text: strategy === '定时' ? `定时 · ${task.cronExpression || '0 0 * * *'}` : strategy })),
      h('td', null,
        h('div', { class: 'muted small', text: task.sourcePath || '-', title: task.sourcePath || '' }),
        h('div', { class: 'muted small', text: `→ ${shortHash(task.targetDirId, 8)}${task.recursive ? '（含子目录）' : ''}` })),
      h('td', null,
        h('div', null, h('span', { class: `status-chip ${runTone}`.trim(), text: runText }),
          h('span', { class: 'muted small', text: task.lastRunAt ? ` ${fmtTime(task.lastRunAt)}` : '' })),
        task.lastRunMessage
          ? h('div', {
            class: task.lastRunStatus === 'BACKUP_RUN_STATUS_FAILED' ? 'error-text small' : 'muted small',
            title: task.lastRunMessage,
            text: task.lastRunMessage,
            style: { maxWidth: '320px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
          })
          : null),
      h('td', { class: 'muted small',
        text: `${Number(task.backedUpCount || 0)} 个 / ${fmtSize0(task.backedUpSize)}` }),
      h('td', { class: 'act' },
        h('span', { class: 'linklike', text: '立即备份', onclick: () => trigger(task) }),
        h('span', { class: 'linklike', text: '文件', onclick: () => showFiles(task) }),
        h('span', { class: 'linklike', text: '编辑', onclick: () => taskDialog(task) }),
        h('span', { class: 'linklike danger', text: '删除', onclick: () => remove(task) })));
  }

  async function trigger(task) {
    try {
      await call('/api/admin/backup/trigger', { id: Number(task.id) });
      toast('已开始备份，稍后刷新查看结果', 'ok');
      setTimeout(() => reload(), 1200);
    } catch (err) {
      toast(`触发失败：${err.rawMessage || err.message}`, 'err');
    }
  }

  async function remove(task) {
    const ok = await confirmDialog(
      `删除备份任务「${task.name}」？\n已备份到网盘的文件不会被删除，只是不再继续同步。`,
      { title: '删除任务', okText: '删除', danger: true });
    if (!ok) return;
    try {
      await call('/api/admin/backup/task/delete', { ids: [Number(task.id)] });
      toast('已删除', 'ok');
      await reload();
    } catch (err) {
      toast(`删除失败：${err.message}`, 'err');
    }
  }

  // 新建 / 编辑
  function taskDialog(task) {
    const isEdit = Boolean(task);
    const name = h('input', { type: 'text', value: task?.name || '', placeholder: '如 站点备份' });
    const sourcePath = h('input', {
      type: 'text',
      value: task?.sourcePath || '',
      placeholder: '/srv/data/www（服务器上的目录）',
    });
    // 新建时默认落到根目录（与展示的"根目录"一致），免得用户以为已经选好了却卡在校验上
    const target = h('span', { class: 'muted small', text: task ? shortHash(task.targetDirId, 12) : '根目录' });
    let targetId = task?.targetDirId || store.rootId || '';
    const strategy = h('select', null,
      ['BACKUP_STRATEGY_SCHEDULED', 'BACKUP_STRATEGY_WATCH', 'BACKUP_STRATEGY_MANUAL'].map((value) =>
        h('option', { value, text: STRATEGY[value] })));
    strategy.value = task?.strategy || 'BACKUP_STRATEGY_SCHEDULED';
    const cron = h('input', { type: 'text', value: task?.cronExpression || '0 0 * * *', placeholder: '分 时 日 月 周' });
    const ignore = h('textarea', { rows: '3', placeholder: '可选，.gitignore 语法，一行一条' });
    ignore.value = task?.ignoreRules || '';
    const recursive = h('input', { type: 'checkbox' });
    recursive.checked = task ? Boolean(task.recursive) : true;
    const enabled = h('input', { type: 'checkbox' });
    enabled.checked = task ? Boolean(task.enabled) : true;

    const cronField = h('div', { class: 'field' },
      h('label', { text: 'Cron 表达式' }),
      cron,
      h('div', { class: 'muted small', text: '分 时 日 月 周，例如 0 3 * * * = 每天 03:00' }));
    const syncCronVisibility = () => {
      cronField.hidden = strategy.value !== 'BACKUP_STRATEGY_SCHEDULED';
    };
    strategy.addEventListener('change', syncCronVisibility);
    syncCronVisibility();

    const modal = openModal({
      title: isEdit ? `编辑备份任务 · ${task.name}` : '新建备份任务',
      body: h('div', null,
        field('任务名称', name),
        field('源路径（服务器本地目录）', sourcePath,
          h('div', { class: 'muted small', text: '必须已存在且是目录；相对路径会被服务端拒绝。' })),
        h('div', { class: 'field' },
          h('label', { text: '备份到' }),
          h('div', { class: 'toolbar', style: { marginBottom: '0' } },
            target,
            h('button', {
              class: 'sm',
              text: '选择目录',
              onclick: async () => {
                const picked = await pickFolder({ title: '备份到', okText: '选这个目录' });
                if (!picked) return;
                targetId = picked.id;
                target.textContent = picked.name || '已选目录';
              },
            }))),
        field('策略', strategy),
        cronField,
        h('div', { class: 'field' }, h('label', { text: '忽略规则' }), ignore),
        h('div', { class: 'field' },
          h('label', null, recursive, h('span', { text: ' 包含子目录' })),
          h('label', null, enabled, h('span', { text: ' 启用' })))),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: isEdit ? '保存' : '创建',
          onclick: () => save(task, { name, sourcePath, targetId: () => targetId, strategy, cron, ignore, recursive, enabled }, modal),
        })),
    });
  }

  async function save(task, form, modal) {
    const name = form.name.value.trim();
    if (!name) {
      toast('请填写任务名称');
      return;
    }
    const sourcePath = form.sourcePath.value.trim();
    if (!sourcePath) {
      toast('请填写源路径');
      return;
    }
    if (!sourcePath.startsWith('/')) {
      toast('源路径要填服务器上的绝对路径（以 / 开头）');
      return;
    }
    const targetId = form.targetId();
    if (!targetId) {
      toast('请选择备份到的目录');
      return;
    }
    const body = {
      name,
      sourcePath,
      targetDirId: targetId,
      strategy: form.strategy.value,
      recursive: form.recursive.checked,
      enabled: form.enabled.checked,
    };
    if (form.strategy.value === 'BACKUP_STRATEGY_SCHEDULED') {
      const cron = form.cron.value.trim();
      if (!cron) {
        toast('定时策略要填 Cron 表达式');
        return;
      }
      body.cronExpression = cron;
    }
    const ignore = form.ignore.value.trim();
    if (ignore) body.ignoreRules = ignore;
    try {
      const path = task ? '/api/admin/backup/task/update' : '/api/admin/backup/task/create';
      if (task) body.id = Number(task.id);
      await call(path, body);
      toast(task ? '已保存' : '已创建', 'ok');
      modal.close();
      await reload();
    } catch (err) {
      // 备份任务的失败原因（源路径不存在 / 不是目录）只有服务端知道，
      // 这里优先显示原始信息，别被统一的"参数不正确"盖掉
      toast(`保存失败：${err.rawMessage || err.message}`, 'err');
    }
  }

  // 备份文件记录
  async function showFiles(task) {
    const listBox = h('div', { class: 'muted small', text: '加载中…' });
    const modal = openModal({
      title: `备份文件 · ${task.name}`,
      body: listBox,
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '关闭', onclick: () => modal.close() })),
      wide: true,
    });
    try {
      const data = await call('/api/admin/backup/files', { taskId: Number(task.id), page: 1, pageSize: 50 });
      if (!alive()) return;
      const files = data.files || [];
      if (!files.length) {
        setChildren(listBox, h('div', { class: 'muted small', text: '还没有备份记录（可以先点「立即备份」）' }));
        return;
      }
      setChildren(listBox,
        h('div', { class: 'list-wrap' }, h('table', { class: 'files' },
          h('thead', null, h('tr', null,
            h('th', { text: '相对路径' }),
            h('th', { class: 'size', text: '大小' }),
            h('th', { text: '状态' }),
            h('th', { text: '修改时间' }))),
          h('tbody', null, files.map((f) => {
            const [text, tone] = FILE_STATUS[f.status] || ['未知', ''];
            return h('tr', null,
              h('td', null, h('div', { class: 'muted small', text: f.relativePath || '-' }),
                f.message ? h('div', { class: 'error-text small', text: f.message }) : null),
              h('td', { class: 'size', text: fmtSize(f.fileSize) }),
              h('td', null, h('span', { class: `status-chip ${tone}`.trim(), text })),
              h('td', { class: 'time', text: fmtTime(f.modifiedAt ? Number(f.modifiedAt) * 1000 : '') }));
          })))),
        h('div', { class: 'muted small', style: { marginTop: '8px' }, text: `共 ${Number(data.total || files.length)} 条，显示最近 ${files.length} 条` }));
    } catch (err) {
      if (!alive()) return;
      setChildren(listBox, h('div', { class: 'error-text', text: `加载失败：${err.message}` }));
    }
  }

  function field(label, input, extra) {
    return h('div', { class: 'field' }, h('label', { text: label }), input, extra || null);
  }
}
