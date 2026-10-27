// 管理 → 存储源（仅管理员）。
//
// 只暴露"配置外壳"：名称 / 类型 / 容量 / 启用 / 优先级 / 大小上限 / 并发 / 代理。
// `config`（refresh_token、client_secret 这些）**一律不回显**，也不在网页里编辑——
// 一是不该把密钥塞进 DOM 和浏览器历史，二是改坏一个 token 的代价比省几步操作大。
// 要换凭据请去客户端（那里的表单会回显并校验）。
//
// 新增存储源用 /api/admin/provider/types 返回的 config_fields 动态生成表单，
// 所以驱动新增字段时这里不用改代码。

import { call } from '../api.js';
import { h, toast, confirmDialog, emptyState, errorState, openModal, setChildren } from '../dom.js';
import { fmtSize, fmtSize0 } from '../format.js';
import { back } from '../router.js';
import { session } from '../session.js';

let renderToken = 0;

export async function renderAdminProviders(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  main.className = 'main';

  const body = h('div');
  const head = h('div', { class: 'page-head' },
    h('h1', { text: '🗄️ 存储源' }),
    h('div', { class: 'spacer' }),
    h('button', { text: '返回文件', onclick: () => back('/files') }));
  setChildren(main, h('div', { class: 'page' }, head, body));

  if (!session.user?.isAdmin) {
    setChildren(body, emptyState('这个页面只有管理员能看', '🔒'));
    return;
  }

  setChildren(body, h('div', { class: 'muted small', text: '加载中…' }));

  let providers = [];
  let types = [];
  try {
    const [listData, typeData] = await Promise.all([
      call('/api/admin/provider/list', {}),
      call('/api/admin/provider/types', {}),
    ]);
    if (!alive()) return;
    providers = listData.providers || [];
    types = typeData.types || [];
  } catch (err) {
    if (!alive()) return;
    setChildren(body, errorState(`加载失败：${err.message}`, () => renderAdminProviders(main, route)));
    return;
  }

  const typeName = (value) => types.find((t) => t.driverId === value)?.displayName || value || '-';
  const tableWrap = h('div');

  setChildren(body,
    h('div', { class: 'toolbar' },
      h('button', { class: 'primary', text: '＋ 新增存储源', onclick: () => addDialog() }),
      h('button', { text: '刷新', onclick: () => renderAdminProviders(main, route) }),
      h('div', { class: 'spacer' }),
      h('span', { class: 'muted small', text: `共 ${providers.length} 个存储源` })),
    tableWrap,
    h('div', { class: 'muted small', style: { marginTop: '10px' },
      text: '凭据类配置（token / client_secret 等）不在网页里回显和编辑，需要更换时请用客户端。' }));

  function renderTable() {
    if (!alive()) return;
    if (!providers.length) {
      setChildren(tableWrap, emptyState('还没有存储源，点上面的「新增存储源」', '🗄️'));
      return;
    }
    setChildren(tableWrap, h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { text: '名称 / 类型' }),
        h('th', { text: '容量' }),
        h('th', { text: '状态' }),
        h('th', { text: '优先级 / 上限' }),
        h('th', { class: 'act' }))),
      h('tbody', null, providers.map(row)))));
  }

  function row(provider) {
    const total = Number(provider.totalCapacity || 0);
    const used = Number(provider.usedCapacity || 0);
    const limit = Number(provider.fileSizeLimit || 0);
    return h('tr', null,
      h('td', null,
        h('div', { class: 'name-cell' },
          h('span', { class: 'ico', text: '🗄️' }),
          h('span', { class: 'label', text: provider.name || `#${provider.id}` }),
          provider.defaultBound ? h('span', { class: 'status-chip', text: '默认绑定' }) : null),
        h('div', { class: 'muted small', text: typeName(provider.driverId) })),
      h('td', { text: total > 0 ? `${fmtSize0(used)} / ${fmtSize(total)}` : `${fmtSize0(used)} / 不限` }),
      h('td', null, h('span', {
        class: `status-chip ${provider.enabled ? 'ok' : 'warn'}`,
        text: provider.enabled ? '启用' : '停用',
      })),
      h('td', { class: 'muted small',
        text: `${provider.sortOrder ?? 0} / ${limit > 0 ? fmtSize(limit) : '不限'}` }),
      h('td', { class: 'act' },
        h('span', {
          class: 'linklike',
          text: provider.enabled ? '停用' : '启用',
          onclick: () => toggleEnabled(provider),
        }),
        h('span', { class: 'linklike', text: '编辑', onclick: () => editDialog(provider) }),
        h('span', { class: 'linklike danger', text: '删除', onclick: () => remove(provider) })));
  }

  async function toggleEnabled(provider) {
    try {
      await call('/api/admin/provider/update', { id: Number(provider.id), enabled: !provider.enabled });
      toast(provider.enabled ? '已停用' : '已启用', 'ok');
      await renderAdminProviders(main, route);
    } catch (err) {
      toast(`操作失败：${err.message}`, 'err');
    }
  }

  function editDialog(provider) {
    const name = h('input', { type: 'text', value: provider.name || '' });
    const desc = h('input', { type: 'text', value: provider.description || '', placeholder: '可选' });
    const sortOrder = h('input', { type: 'number', min: '0', value: String(provider.sortOrder ?? 0) });
    const limitGB = h('input', {
      type: 'number', min: '0', step: '0.1',
      value: Number(provider.fileSizeLimit || 0) > 0 ? (Number(provider.fileSizeLimit) / (1024 ** 3)).toFixed(2) : '',
      placeholder: '留空或 0 表示不限',
    });
    const concurrent = h('input', { type: 'number', min: '0', value: String(provider.maxConcurrentUploads ?? 0) });
    const proxy = h('input', { type: 'text', value: provider.proxy || '', placeholder: '可选，如 http://host:port' });

    const modal = openModal({
      title: `编辑存储源 · ${provider.name}`,
      body: h('div', null,
        field('名称', name),
        field('描述', desc),
        field('优先级（越小越优先）', sortOrder),
        field('单文件大小上限（GB）', limitGB),
        field('最大并发上传（0 = 跟随全局）', concurrent),
        field('代理地址', proxy),
        h('div', { class: 'muted small', text: '类型与凭据不在网页里修改；改凭据请用客户端。' })),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '保存',
          onclick: () => saveEdit(provider, { name, desc, sortOrder, limitGB, concurrent, proxy }, modal),
        })),
    });
  }

  async function saveEdit(provider, inputs, modal) {
    const body = { id: Number(provider.id) };
    if (inputs.name.value.trim()) body.name = inputs.name.value.trim();
    body.description = inputs.desc.value.trim();
    const sortOrder = Number(inputs.sortOrder.value || 0);
    if (Number.isFinite(sortOrder) && sortOrder >= 0) body.sortOrder = Math.floor(sortOrder);
    const gb = Number(inputs.limitGB.value || 0);
    if (inputs.limitGB.value.trim() !== '' && (!Number.isFinite(gb) || gb < 0)) {
      toast('单文件大小上限要填不小于 0 的数字（GB）');
      return;
    }
    body.fileSizeLimit = String(Math.round((Number.isFinite(gb) ? gb : 0) * 1024 ** 3));
    const concurrent = Number(inputs.concurrent.value || 0);
    if (Number.isFinite(concurrent) && concurrent >= 0) body.maxConcurrentUploads = Math.floor(concurrent);
    body.proxy = inputs.proxy.value.trim();
    try {
      await call('/api/admin/provider/update', body);
      toast('已保存', 'ok');
      modal.close();
      await renderAdminProviders(main, route);
    } catch (err) {
      toast(`保存失败：${err.message}`, 'err');
    }
  }

  async function remove(provider) {
    const ok = await confirmDialog(
      `删除存储源「${provider.name}」？\n只删除这条配置，不会删云端已有的文件；但没有其它存储源接手时，那些文件将无法再读取。`,
      { title: '删除存储源', okText: '删除', danger: true });
    if (!ok) return;
    try {
      await call('/api/admin/provider/delete', { ids: [Number(provider.id)] });
      toast('已删除', 'ok');
      await renderAdminProviders(main, route);
    } catch (err) {
      toast(`删除失败：${err.message}`, 'err');
    }
  }

  function field(label, input) {
    return h('div', { class: 'field' }, h('label', { text: label }), input);
  }

  // 新增：类型 → 按 config_fields 动态生成表单
  function addDialog() {
    if (!types.length) {
      toast('拿不到存储源类型列表，稍后再试', 'err');
      return;
    }
    const typeSelect = h('select', null, types.map((t) =>
      h('option', { value: t.driverId, text: t.displayName || t.name || t.driverId })));
    const name = h('input', { type: 'text', placeholder: '实例名称，如 我的阿里云盘' });
    const desc = h('input', { type: 'text', placeholder: '可选' });
    const fieldsBox = h('div');
    let inputs = [];

    const buildFields = () => {
      const type = types.find((t) => t.driverId === typeSelect.value);
      inputs = (type?.configFields || []).map((f) => {
        const input = inputFor(f);
        input.dataset.name = f.name;
        input.dataset.fieldType = f.fieldType || 'STRING';
        return { spec: f, input };
      });
      setChildren(fieldsBox, inputs.map(({ spec, input }) =>
        h('div', { class: 'field' },
          h('label', { text: `${spec.label || spec.name}${spec.required ? ' *' : ''}` }),
          input,
          spec.helperText ? h('div', { class: 'muted small', text: spec.helperText }) : null)));
    };
    typeSelect.addEventListener('change', buildFields);
    buildFields();

    const modal = openModal({
      title: '新增存储源',
      body: h('div', null,
        field('类型', typeSelect),
        field('名称', name),
        field('描述', desc),
        fieldsBox),
      footer: h('div', { class: 'toolbar', style: { marginBottom: '0' } },
        h('button', { text: '取消', onclick: () => modal.close() }),
        h('button', {
          class: 'primary',
          text: '添加',
          onclick: () => submitAdd(typeSelect, name, desc, inputs, modal),
        })),
    });
  }

  function inputFor(spec) {
    const type = spec.fieldType || 'STRING';
    if (type === 'BOOLEAN') {
      const input = h('input', { type: 'checkbox' });
      input.checked = spec.defaultValue === 'true';
      return input;
    }
    if (type === 'SELECT') {
      return h('select', null, (spec.selectOptions || []).map((o) =>
        h('option', { value: o.value, text: o.label || o.value })));
    }
    if (type === 'NUMBER') {
      return h('input', { type: 'number', value: spec.defaultValue || '' });
    }
    // 凭据类字段（token / secret）用密码框，避免在屏幕上明文常驻
    const secret = /token|secret|password|cookie|key$/i.test(spec.name || '');
    return h('input', {
      type: secret ? 'password' : 'text',
      value: spec.defaultValue || '',
      placeholder: spec.placeholder || '',
      autocomplete: 'off',
    });
  }

  async function submitAdd(typeSelect, name, desc, inputs, modal) {
    const providerName = name.value.trim();
    if (!providerName) {
      toast('请填写实例名称');
      return;
    }
    const config = {};
    for (const { spec, input } of inputs) {
      const type = spec.fieldType || 'STRING';
      let value;
      if (type === 'BOOLEAN') value = input.checked;
      else if (type === 'NUMBER') {
        const text = String(input.value || '').trim();
        if (text === '') {
          if (spec.required) {
            toast(`「${spec.label || spec.name}」必填`);
            return;
          }
          continue;
        }
        value = Number(text);
        if (!Number.isFinite(value)) {
          toast(`「${spec.label || spec.name}」要填数字`);
          return;
        }
      } else if (type === 'STRING_LIST') {
        value = String(input.value || '').split(',').map((s) => s.trim()).filter(Boolean);
      } else {
        value = String(input.value || '');
        if (!value.trim()) {
          if (spec.required) {
            toast(`「${spec.label || spec.name}」必填`);
            return;
          }
          continue;
        }
      }
      config[spec.name] = value;
    }
    try {
      await call('/api/admin/provider/add', {
        driverId: typeSelect.value,
        name: providerName,
        description: desc.value.trim(),
        config: JSON.stringify(config),
        enabled: true,
      });
      toast('已添加', 'ok');
      modal.close();
      await renderAdminProviders(main, route);
    } catch (err) {
      toast(`添加失败：${err.message}`, 'err');
    }
  }

  renderTable();
}
