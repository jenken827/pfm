// 文件列表页：目录浏览 / 最近 / 按类型 / 搜索结果，四种来源共用一套渲染。
//
// 交互与客户端对齐：文件夹优先、点名字打开、多选批量操作、列表 / 网格切换、
// 排序记忆在 localStorage；"上传"在浏览器端没有对应能力（BLAKE3 分片在客户端做），
// 因此按钮置灰并说明原因，而不是藏起来让人以为坏了。

import { call, downloadUrl } from '../api.js';
import {
  h, toast, confirmDialog, promptDialog, triggerDownload, emptyState, errorState, setChildren, openMenu,
} from '../dom.js';
import {
  fmtSize, fmtTime, iconFor, isDir, typeText, sortFiles,
} from '../format.js';
import { navigate } from '../router.js';
import { ensureSpace, store, setView, setSort, refreshSpace } from '../store.js';
import { ThumbLoader } from '../thumbs.js';
import { pickFolder } from '../components/picker.js';

const PAGE_SIZE = 200;
const TYPE_FILTERS = ['IMAGE', 'VIDEO', 'AUDIO', 'DOCUMENT', 'ARCHIVE', 'APPLICATION'];

let renderToken = 0;

/**
 * 渲染列表页。
 * @param {HTMLElement} main 主内容容器
 * @param {{name: string, params: object, query: URLSearchParams}} route
 */
export async function renderFiles(main, route) {
  renderToken += 1;
  const token = renderToken;
  const alive = () => token === renderToken;

  const state = {
    files: [],
    selected: new Set(),
    offset: 0,
    total: 0,
    hasMore: false,
    loading: false,
    folderId: '',
    crumbs: [],
    mode: route.name,
    query: route.query.get('q') || '',
    type: route.params.type || '',
  };

  const head = h('div', { class: 'page-head' });
  const toolbar = h('div', { class: 'toolbar' });
  const content = h('div');
  const batch = h('div');
  const thumbs = new ThumbLoader();

  setChildren(main, h('div', { class: 'page' }, head, toolbar, content, batch));

  // ---------------------------------------------------------------- 数据加载

  async function resolveContext() {
    await ensureSpace();
    if (state.mode !== 'files') return;
    state.folderId = route.params.id || store.rootId;
    state.crumbs = await loadCrumbs(state.folderId);
  }

  /** 从当前目录沿 parentId 上溯，拼出面包屑（最多 32 层，防环） */
  async function loadCrumbs(folderId) {
    const crumbs = [];
    let id = folderId;
    for (let depth = 0; depth < 32 && id && id !== '0'; depth += 1) {
      if (id === store.rootId) {
        crumbs.push({ id, name: '根目录' });
        break;
      }
      try {
        const info = await call('/api/file/info', { fileId: id });
        crumbs.push({ id, name: info.name || '(未命名)' });
        id = info.parentId || '';
      } catch {
        crumbs.push({ id, name: '(未知目录)' });
        break;
      }
    }
    return crumbs.reverse();
  }

  function fetchPage(offset) {
    if (state.mode === 'recent') {
      return call('/api/file/recent', { offset, pageSize: PAGE_SIZE });
    }
    if (state.mode === 'type') {
      return call('/api/file/by-type', { fileType: state.type, offset, pageSize: PAGE_SIZE });
    }
    if (state.mode === 'search') {
      return call('/api/file/search', { keyword: state.query, searchType: 'NAME', offset, pageSize: PAGE_SIZE });
    }
    return call('/api/file/list', { parentId: state.folderId, offset, pageSize: PAGE_SIZE });
  }

  async function load(reset) {
    if (state.loading) return;
    state.loading = true;
    const offset = reset ? 0 : state.offset;
    if (reset) {
      setChildren(content, h('div', { class: 'empty' }, h('div', { class: 'spinner', style: { margin: '0 auto' } })));
    }
    try {
      const data = await fetchPage(offset);
      if (!alive()) return;
      const files = data.files || [];
      state.files = reset ? files : state.files.concat(files);
      state.offset = offset + files.length;
      state.total = Number(data.total) || state.files.length;
      state.hasMore = Boolean(data.hasMore);
      if (reset) state.selected.clear();
      renderList();
    } catch (err) {
      if (!alive()) return;
      setChildren(content, errorState(`加载失败：${err.message}`, () => load(true)));
    } finally {
      state.loading = false;
    }
  }

  // ---------------------------------------------------------------- 头部 / 工具栏

  function renderHead() {
    setChildren(head);
    if (state.mode === 'files') {
      const crumb = h('nav', { class: 'crumb' });
      state.crumbs.forEach((c, i) => {
        if (i > 0) crumb.appendChild(h('span', { class: 'sep', text: '/' }));
        const last = i === state.crumbs.length - 1;
        crumb.appendChild(last
          ? h('span', { class: 'cur', text: c.name })
          : h('a', { href: `#/files/${encodeURIComponent(c.id)}`, text: c.name }));
      });
      head.appendChild(crumb);
      head.appendChild(h('div', { class: 'spacer' }));
      head.appendChild(h('div', { class: 'muted small', text: `共 ${state.total} 项` }));
      return;
    }
    const titles = {
      recent: ['最近访问', '🕘'],
      type: [`${typeText(state.type)}`, '🏷️'],
      search: [`搜索“${state.query}”`, '🔍'],
    };
    const [title, icon] = titles[state.mode] || ['文件', '📁'];
    head.appendChild(h('h1', { text: `${icon} ${title}` }));
    head.appendChild(h('div', { class: 'spacer' }));
    head.appendChild(h('div', { class: 'muted small', text: `共 ${state.total} 项` }));
  }

  function renderToolbar() {
    setChildren(toolbar);
    if (state.mode === 'files') {
      toolbar.appendChild(h('button', {
        class: 'primary',
        text: '＋ 新建文件夹',
        onclick: () => createDir(state.folderId, () => load(true)),
      }));
      toolbar.appendChild(h('button', {
        text: '上传',
        disabled: true,
        title: '浏览器端暂不支持上传：分片与 BLAKE3 校验在客户端完成，请用 App 上传',
      }));
      toolbar.appendChild(h('button', { text: '刷新', onclick: () => load(true) }));
      if (state.folderId) {
        toolbar.appendChild(h('button', {
          text: '目录设置',
          onclick: () => navigate(`/settings/${encodeURIComponent(state.folderId)}`),
        }));
      }
    } else {
      toolbar.appendChild(h('button', { text: '刷新', onclick: () => load(true) }));
    }

    toolbar.appendChild(h('div', { class: 'spacer' }));

    const sortSel = h('select', {
      style: { width: 'auto' },
      title: '排序字段',
      onchange: (ev) => {
        setSort(ev.target.value);
        renderList();
      },
    },
    ...[['name', '名称'], ['time', '时间'], ['size', '大小'], ['type', '类型']].map(([v, label]) =>
      h('option', { value: v, text: label, selected: store.sortKey === v })));

    toolbar.appendChild(sortSel);
    toolbar.appendChild(h('button', {
      class: 'icon',
      text: store.sortDir === 1 ? '↑' : '↓',
      title: store.sortDir === 1 ? '升序（点击切换）' : '降序（点击切换）',
      onclick: () => {
        store.sortDir = store.sortDir === 1 ? -1 : 1;
        renderList();
      },
    }));

    const seg = h('div', { class: 'seg' },
      h('button', {
        class: store.view === 'list' ? 'on' : '',
        text: '☰ 列表',
        onclick: () => { setView('list'); renderToolbar(); renderList(); },
      }),
      h('button', {
        class: store.view === 'grid' ? 'on' : '',
        text: '▦ 网格',
        onclick: () => { setView('grid'); renderToolbar(); renderList(); },
      }));
    toolbar.appendChild(seg);
  }

  // ---------------------------------------------------------------- 列表渲染

  function renderList() {
    if (!alive()) return;
    renderHead();
    renderToolbar();
    thumbs.reset();
    setChildren(content);

    const files = sortFiles(state.files, store.sortKey, store.sortDir);
    if (!files.length) {
      content.appendChild(emptyState(
        state.mode === 'search' ? '没有匹配的文件' : '这里还没有文件',
        state.mode === 'search' ? '🔍' : '📂'));
    } else if (store.view === 'grid') {
      content.appendChild(renderGrid(files));
    } else {
      content.appendChild(renderTable(files));
    }

    if (state.hasMore) {
      content.appendChild(h('div', { style: { textAlign: 'center', marginTop: '14px' } },
        h('button', {
          text: `加载更多（${state.offset}/${state.total}）`,
          onclick: (ev) => {
            ev.target.disabled = true;
            load(false);
          },
        })));
    }
    renderBatch();
  }

  function renderTable(files) {
    const allChecked = files.length > 0 && files.every((f) => state.selected.has(f.id));
    const headBox = h('input', {
      type: 'checkbox',
      checked: allChecked,
      title: '全选本页',
      onchange: (ev) => {
        files.forEach((f) => (ev.target.checked ? state.selected.add(f.id) : state.selected.delete(f.id)));
        renderList();
      },
    });

    const body = h('tbody');
    files.forEach((f) => body.appendChild(tableRow(f)));
    return h('div', { class: 'card list-wrap' }, h('table', { class: 'files' },
      h('thead', null, h('tr', null,
        h('th', { class: 'check' }, headBox),
        h('th', { text: '名称' }),
        h('th', { class: 'size', text: '大小' }),
        h('th', { class: 'time', text: '修改时间' }),
        h('th', { class: 'act' }))),
      body));
  }

  function tableRow(f) {
    const selected = state.selected.has(f.id);
    const box = h('input', {
      type: 'checkbox',
      checked: selected,
      onchange: (ev) => {
        ev.stopPropagation();
        toggleSelect(f.id, ev.target.checked);
      },
    });
    // 图片在列表里也给个小缩略图（失败则退回图标）
    let ico = h('span', { class: 'ico', text: iconFor(f) });
    if (f.type === 'IMAGE') {
      const fallback = ico;
      const img = h('img', { class: 'ico-thumb', alt: '', loading: 'lazy' });
      thumbs.load(f.id, (url) => {
        img.src = url;
        img.addEventListener('error', () => img.replaceWith(fallback), { once: true });
      });
      ico = img;
    }

    const nameCell = h('div', { class: 'name-cell', title: f.name || '' },
      ico,
      h('span', { class: 'label', text: f.name || '(未命名)' }),
      f.isReady === false && !isDir(f) ? h('span', { class: 'badge warn', text: '未就绪' }) : null);
    nameCell.addEventListener('click', () => openFile(f));

    const actions = h('td', { class: 'act' },
      isDir(f)
        ? h('a', { text: '打开', href: `#/files/${encodeURIComponent(f.id)}` })
        : h('span', {
          class: 'linklike',
          text: '下载',
          onclick: () => downloadOne(f),
        }),
      h('a', {
        text: '详情',
        href: `#/detail/${encodeURIComponent(f.id)}`,
      }),
      h('button', {
        class: 'row-menu',
        text: '⋮',
        title: '更多操作',
        'aria-haspopup': 'menu',
        onclick: (ev) => {
          ev.stopPropagation();
          openRowMenu(ev.currentTarget, f);
        },
      }));

    return h('tr', { class: selected ? 'selected' : '' },
      h('td', { class: 'check' }, box),
      h('td', null, nameCell),
      h('td', { class: 'size', text: isDir(f) ? '-' : fmtSize(f.size) }),
      h('td', { class: 'time', text: fmtTime(f.updatedAt || f.createdAt) }),
      actions);
  }

  function renderGrid(files) {
    const grid = h('div', { class: 'grid' });
    files.forEach((f) => grid.appendChild(tile(f)));
    return grid;
  }

  function tile(f) {
    const selected = state.selected.has(f.id);
    const thumb = h('div', { class: 'thumb' }, h('span', { text: iconFor(f) }));
    if (f.type === 'IMAGE') {
      const img = h('img', { alt: '', loading: 'lazy' });
      setChildren(thumb, img);
      thumbs.load(f.id, (url) => {
        img.src = url;
        img.addEventListener('error', () => setChildren(thumb, h('span', { text: iconFor(f) })), { once: true });
      });
    }
    const pick = h('input', {
      class: 'pick',
      type: 'checkbox',
      checked: selected,
      onclick: (ev) => ev.stopPropagation(),
      onchange: (ev) => toggleSelect(f.id, ev.target.checked),
    });

    const node = h('div', { class: `tile${selected ? ' selected' : ''}` },
      pick,
      thumb,
      h('div', { class: 'cap' },
        h('div', { class: 't', text: f.name || '(未命名)', title: f.name || '' }),
        h('div', { class: 's', text: isDir(f) ? '文件夹' : `${fmtSize(f.size)} · ${fmtTime(f.updatedAt || f.createdAt)}` })));
    node.addEventListener('click', () => openFile(f));
    // 右键（桌面）/ 长按（移动端由 contextmenu 事件模拟）出操作菜单
    node.addEventListener('contextmenu', (ev) => {
      ev.preventDefault();
      openRowMenu(node, f);
    });
    return node;
  }

  // ---------------------------------------------------------------- 选择 / 批量

  function toggleSelect(id, on) {
    if (on) state.selected.add(id);
    else state.selected.delete(id);
    renderList();
  }

  function renderBatch() {
    setChildren(batch);
    if (!state.selected.size) return;
    const ids = Array.from(state.selected);
    const items = state.files.filter((f) => state.selected.has(f.id));
    const fileCount = items.filter((f) => !isDir(f)).length;

    const bar = h('div', { class: 'batchbar' },
      h('strong', { text: `已选 ${ids.length} 项` }),
      h('button', {
        class: 'sm',
        text: `下载${fileCount ? `（${fileCount}）` : ''}`,
        disabled: fileCount === 0,
        onclick: () => downloadMany(items),
      }),
      h('button', { class: 'sm', text: '移动', onclick: () => moveMany(ids) }),
      h('button', {
        class: 'sm danger',
        text: '删除',
        onclick: () => deleteMany(ids),
      }),
      h('div', { class: 'spacer' }),
      h('button', {
        class: 'sm ghost',
        text: '取消选择',
        onclick: () => {
          state.selected.clear();
          renderList();
        },
      }),
      h('button', {
        class: 'sm ghost',
        text: '全选本页',
        onclick: () => {
          state.files.forEach((f) => state.selected.add(f.id));
          renderList();
        },
      }));
    batch.appendChild(bar);
  }

  // ---------------------------------------------------------------- 操作

  function openFile(f) {
    if (isDir(f)) {
      navigate(`/files/${encodeURIComponent(f.id)}`);
      return;
    }
    // 预览页自己决定能否预览（不能则跳详情），这里只负责把"来源目录"带过去，
    // 好让图片能左右翻页看同目录的其它图片。
    const from = state.mode === 'files' ? state.folderId : '';
    navigate(`/preview/${encodeURIComponent(f.id)}` + (from ? `?from=${encodeURIComponent(from)}` : ''));
  }

  async function downloadOne(f, providerId) {
    try {
      const url = await downloadUrl(f.id, { providerId });
      triggerDownload(url);
    } catch (err) {
      toast(`下载失败：${err.message}`, 'err');
    }
  }

  async function downloadMany(items) {
    const files = items.filter((f) => !isDir(f));
    if (!files.length) return;
    toast(`开始下载 ${files.length} 个文件`);
    for (const f of files) {
      // 串行：浏览器对同域并发下载有上限，串行反而更稳、顺序也可预期
      await downloadOne(f);
    }
  }

  /** 文件行 / 网格项的「⋮」菜单：单文件的重命名、移动、删除（批量条之外的单条入口） */
  function openRowMenu(anchor, f) {
    const items = [];
    if (isDir(f)) {
      items.push({ text: '打开', onClick: () => navigate(`/files/${encodeURIComponent(f.id)}`) });
      items.push({ text: '目录设置', onClick: () => navigate(`/settings/${encodeURIComponent(f.id)}`) });
    } else {
      items.push({ text: '预览', onClick: () => openFile(f) });
      items.push({ text: '下载', onClick: () => downloadOne(f) });
    }
    items.push({ text: '重命名', onClick: () => renameOne(f) });
    items.push({ text: '移动到…', onClick: () => moveMany([f.id]) });
    items.push({ text: '详情', onClick: () => navigate(`/detail/${encodeURIComponent(f.id)}`) });
    items.push({ text: '移入回收站', danger: true, onClick: () => deleteMany([f.id]) });
    openMenu(anchor, items);
  }

  async function renameOne(f) {
    const name = await promptDialog({
      title: '重命名',
      label: '新名称',
      value: f.name || '',
      okText: '保存',
      validate: (v) => {
        if (!v) return '名称不能为空';
        if (v === '.' || v === '..') return '名称不能是 . 或 ..';
        if (v.includes('/')) return '名称不能包含 /';
        if (v === f.name) return '名称没有变化';
        return '';
      },
    });
    if (name == null) return;
    try {
      await call('/api/file/rename', { renameObjs: [{ id: f.id, newName: name }] });
      toast('已重命名', 'ok');
      load(true);
    } catch (err) {
      toast(`重命名失败：${err.message}`, 'err');
    }
  }

  async function moveMany(ids) {
    const picked = await pickFolder({ title: `移动 ${ids.length} 项到` });
    const target = picked?.id || '';
    if (!target) return;
    try {
      const data = await call('/api/file/move', { fileIds: ids, targetParentId: target });
      const failed = (data.failedIds || []).length;
      if (failed) toast(`已移动 ${data.successCount || 0} 项，${failed} 项失败`, 'err');
      else toast('移动完成', 'ok');
      state.selected.clear();
      load(true);
    } catch (err) {
      toast(`移动失败：${err.message}`, 'err');
    }
  }

  async function deleteMany(ids) {
    const ok = await confirmDialog(
      `确定把 ${ids.length} 项移到回收站吗？可以在回收站里恢复。`,
      { title: '移入回收站', okText: '删除', danger: true },
    );
    if (!ok) return;
    try {
      await call('/api/file/delete', { ids });
      toast('已移入回收站', 'ok');
      state.selected.clear();
      refreshSpace();
      load(true);
    } catch (err) {
      toast(`删除失败：${err.message}`, 'err');
    }
  }

  // ---------------------------------------------------------------- 启动

  try {
    await resolveContext();
  } catch (err) {
    if (alive()) setChildren(content, errorState(`加载失败：${err.message}`, () => renderFiles(main, route)));
    return;
  }
  if (!alive()) return;
  renderHead();
  renderToolbar();
  await load(true);

  // 页面切走时释放缩略图 objectURL
  window.addEventListener('hashchange', () => thumbs.dispose(), { once: true });
}

/** 新建文件夹（列表页与目录设置页共用）；成功后回调刷新 */
export async function createDir(parentId, onDone) {
  const name = await promptDialog({
    title: '新建文件夹',
    label: '文件夹名称',
    placeholder: '例如 照片 / 工作',
    okText: '创建',
    validate: (v) => {
      if (!v) return '名称不能为空';
      if (v === '.' || v === '..') return '名称不能是 . 或 ..';
      if (v.includes('/')) return '名称不能包含 /';
      return '';
    },
  });
  if (!name) return;
  try {
    await call('/api/file/create-dir', { name, parentId });
    toast('文件夹已创建', 'ok');
    if (onDone) onDone();
  } catch (err) {
    toast(`创建失败：${err.message}`, 'err');
  }
}

/** 供侧边栏使用：按类型页的入口列表 */
export const FILE_TYPE_FILTERS = TYPE_FILTERS;
