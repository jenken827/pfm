// 目录选择器（移动文件 / 恢复回收站 / 离线下载目标目录共用）。
//
// 懒加载：点开一个节点才请求它的子文件夹，避免一次拉全盘目录树。
// 只能选文件夹，根目录永远可选（"移到根目录"是常见需求）。

import { call } from '../api.js';
import { ensureSpace } from '../store.js';
import { h, openModal } from '../dom.js';

/**
 * 选择目标文件夹。
 * @param {object} opts
 * @param {string} opts.title 弹层标题
 * @param {Set<string>} [opts.exclude] 不可选的 id（如正在移动的目录自身）
 * @param {string} [opts.okText]
 * @returns {Promise<{id: string, name: string}|null>} 选中的目录（取消为 null）；
 *   带上 name 是为了让调用方显示"文档/2026"这类可读名字，而不是一串 id。
 */
export function pickFolder({ title = '选择目标文件夹', exclude, okText = '移到这里' } = {}) {
  return new Promise((resolve) => {
    let selected = '';
    let selectedName = '';
    let done = false;
    const tree = h('div', { class: 'picker' });
    const status = h('div', { class: 'hint' });
    const footerOk = h('button', { class: 'primary', text: okText, disabled: true });

    const finish = (value) => {
      if (done) return;
      done = true;
      handle.close();
      resolve(value);
    };

    const confirm = () => {
      if (!selected) return;
      finish({ id: selected, name: selectedName });
    };
    footerOk.addEventListener('click', confirm);

    const handle = openModal({
      title,
      body: [tree, status],
      footer: [
        h('button', { text: '取消', onclick: () => finish(null) }),
        footerOk,
      ],
      onClose: () => finish(null),
    });

    const select = (id, node, name = '') => {
      if (exclude?.has(id)) return;
      selected = id;
      selectedName = name;
      tree.querySelectorAll('.node.on').forEach((n) => n.classList.remove('on'));
      node.classList.add('on');
      footerOk.disabled = false;
      status.textContent = '';
    };

    const makeNode = (folder, depth) => {
      const row = h('div', {
        class: 'node',
        style: { paddingLeft: `${10 + depth * 16}px` },
        dataset: { id: folder.id },
      });
      const children = h('div');
      const arrow = h('span', { class: 'arrow', text: '▸' });
      row.append(arrow, h('span', { text: '📁' }), h('span', { text: folder.name || '(未命名)' }));

      row.addEventListener('click', async (ev) => {
        ev.stopPropagation();
        select(folder.id, row, folder.name || '');
        if (children.childElementCount) {
          const opened = !children.hidden;
          children.hidden = opened;
          arrow.textContent = opened ? '▸' : '▾';
          return;
        }
        arrow.textContent = '…';
        try {
          const data = await call('/api/file/folders', { parentId: folder.id });
          const folders = data.folders || [];
          arrow.textContent = '▾';
          if (!folders.length) {
            status.textContent = '该目录下没有子文件夹';
            return;
          }
          folders.forEach((child) => children.appendChild(makeNode(child, depth + 1)));
          row.after(children);
        } catch (err) {
          arrow.textContent = '▸';
          status.textContent = `读取子目录失败：${err.message}`;
        }
      });
      return row;
    };

    (async () => {
      try {
        const space = await ensureSpace();
        const rootRow = makeNode({ id: space.rootFileId, name: '根目录' }, 0);
        tree.appendChild(rootRow);
        select(space.rootFileId, rootRow, '根目录');
      } catch (err) {
        tree.appendChild(h('div', { class: 'empty', text: `加载失败：${err.message}` }));
      }
    })();
  });
}
