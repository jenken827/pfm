// 目录设置：名称 / 存储源绑定 / 加密 / 历史上限。
//
// 与客户端 FolderSettingsDialog 同口径：
//   1. 名称必填；
//   2. 存储源预选顺序 = 目录自身绑定 → 父目录绑定 → 全部可用；
//   3. 可用存储源非空时至少要选一个；
//   4. 历史上限留空 = 不修改这一项（请求里不带该字段），填了必须是 >= 0 的整数；
//      **0 表示不保留历史版本**（与客户端同一语义），不是"不限制"。
//
// ⚠ 加密开关只影响**之后**写入这个目录的文件；已经存在的文件按上传时的设置读，
// 所以这里给一句提示，避免用户以为打开开关就能给老文件加密。

import { call } from '../api.js';
import { h, toast, errorState, setChildren } from '../dom.js';
import { back, navigate } from '../router.js';
import { loadProviders } from '../store.js';
import { parseProviderIds } from '../components/provider_ids.js';

export async function renderSettings(main, route) {
  const folderId = route.params.id;
  main.className = 'main';

  let detail;
  try {
    detail = await call('/api/file/detail', { fileId: folderId });
  } catch (err) {
    setChildren(main, h('div', { class: 'page' },
      h('div', { class: 'page-head' }, h('h1', { text: '目录设置' })),
      errorState(`加载失败：${err.message}`, () => renderSettings(main, route))));
    return;
  }

  const folder = detail.file || {};
  if (folder.type !== 'FOLDER') {
    navigate(`/detail/${encodeURIComponent(folderId)}`, { replace: true });
    return;
  }

  let providers = [];
  try {
    providers = await loadProviders();
  } catch {
    providers = [];
  }

  // 预选：目录绑定 → 父目录绑定 → 全部可用（与客户端一致）
  let selected = parseProviderIds(folder.boundProviderIds);
  if (!selected.length && folder.parentId) {
    try {
      const info = await call('/api/file/info', { fileId: folder.parentId });
      selected = parseProviderIds(info.boundProviderIds);
    } catch {
      selected = [];
    }
  }
  const available = providers.map((p) => Number(p.id));
  selected = selected.filter((id) => available.includes(id));
  if (!selected.length) selected = [...available];

  const nameInput = h('input', { type: 'text', value: folder.name || '' });
  const encrypt = h('input', { type: 'checkbox', checked: Boolean(folder.enableEncryption) });
  const maxHist = h('input', {
    type: 'text',
    value: folder.maxHistories ? String(folder.maxHistories) : '',
    placeholder: '例如 10（留空表示不修改）',
  });

  const providerBoxes = providers.map((p) => {
    const box = h('input', { type: 'checkbox', checked: selected.includes(Number(p.id)) });
    box.dataset.providerId = String(p.id);
    return h('label', { class: 'check rowitem', style: { cursor: 'pointer' } },
      box,
      h('span', { class: 'grow', text: `#${p.id} ${p.name || ''}（${p.driverId || ''}）` }));
  });

  const save = h('button', {
    class: 'primary',
    text: '保存设置',
    onclick: async (ev) => {
      const name = nameInput.value.trim();
      if (!name) {
        toast('目录名称不能为空');
        return;
      }
      const ids = providerBoxes
        .map((label) => label.querySelector('input'))
        .filter((b) => b.checked)
        .map((b) => Number(b.dataset.providerId));
      if (providers.length && !ids.length) {
        toast('至少绑定一个存储源');
        return;
      }
      let maxHistories;
      if (maxHist.value.trim()) {
        maxHistories = Number(maxHist.value.trim());
        if (!Number.isInteger(maxHistories) || maxHistories < 0) {
          toast('历史版本上限必须是不小于 0 的整数');
          return;
        }
      }

      ev.target.disabled = true;
      try {
        const body = {
          id: folderId,
          name,
          enableEncryption: encrypt.checked,
          providerIds: ids,
        };
        // 留空 = 不改这一项：带上 0 会被服务端理解成"不保留历史版本"，
        // 下次覆盖同名文件就把旧版本全清了（客户端也是留空不发该字段）。
        if (maxHistories !== undefined) body.maxHistories = maxHistories;
        await call('/api/file/update-dir', body);
        toast('设置已保存', 'ok');
        navigate(`/files/${encodeURIComponent(folderId)}`);
      } catch (err) {
        toast(`保存失败：${err.message}`, 'err');
        ev.target.disabled = false;
      }
    },
  });

  setChildren(main, h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('button', { class: 'ghost', text: '← 返回', onclick: () => back(`/files/${encodeURIComponent(folderId)}`) }),
      h('h1', { text: '📁 目录设置' }),
      h('div', { class: 'muted small', text: folder.name || '' }),
      h('div', { class: 'spacer' }),
      save),
    h('div', { class: 'card', style: { padding: '16px' } },
      h('div', { class: 'field' }, h('label', { text: '目录名称' }), nameInput),
      h('div', { class: 'field' },
        h('label', { text: '绑定存储源（新文件会写到这些存储）' }),
        providerBoxes.length ? providerBoxes : h('div', { class: 'muted small', text: '没有可用的存储源' })),
      h('div', { class: 'field' },
        h('label', { class: 'check' }, encrypt, h('span', { text: '加密存储' })),
        h('div', { class: 'hint', text: '只对之后上传到这个目录的文件生效；已存在的文件按各自上传时的设置读取。' })),
      h('div', { class: 'field' },
        h('label', { text: '历史版本上限' }),
        maxHist,
        h('div', { class: 'hint', text: '覆盖同名文件时保留多少个旧版本，超出后淘汰最旧的；填 0 表示不保留历史版本，留空则不改动这一项。' })))));
}
