// 纯文本预览：扩展名白名单 + 字节解码 + 轻量 Markdown 渲染。
//
// ⚠ 白名单与 `frontend/lib/utils/text_preview.dart` 必须完全一致：两端对"哪些文件
// 按文本打开"的判断不能有分叉。Go 侧有对照测试（server/webui_test.go）读这两个
// 文件比对集合，改一处必须改另一处。

export const TEXT_MAX_BYTES = 1024 * 1024;

export const TEXT_EXTENSIONS = new Set([
  // 配置 / 数据
  'json', 'jsonc', 'json5', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf',
  'config', 'properties', 'env', 'xml', 'plist', 'csv', 'tsv', 'ndjson',
  'graphql', 'gql', 'proto', 'thrift', 'avsc',
  // 代码
  'dart', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'vue', 'svelte', 'astro',
  'py', 'pyi', 'rb', 'php', 'go', 'rs', 'java', 'kt', 'kts', 'scala', 'groovy',
  'gradle', 'clj', 'cljs', 'ex', 'exs', 'erl', 'hrl', 'hs', 'lua', 'pl', 'pm',
  'r', 'jl', 'swift', 'm', 'mm', 'c', 'h', 'cc', 'cpp', 'cxx', 'hpp', 'hh',
  'cs', 'fs', 'vb', 'asm', 's', 'sol', 'v', 'zig', 'nim', 'cr', 'd',
  // 脚本
  'sh', 'bash', 'zsh', 'fish', 'ksh', 'bat', 'cmd', 'ps1', 'psm1', 'awk',
  'sed', 'tcl', 'vim',
  // 标记 / 文档 / 文本
  'md', 'markdown', 'mdx', 'rst', 'org', 'adoc', 'txt', 'text', 'log', 'out',
  'err', 'diff', 'patch', 'tex', 'bib', 'srt', 'vtt', 'ass',
  // 构建 / 基础设施
  'cmake', 'mk', 'make', 'bzl', 'bazel', 'tf', 'tfvars', 'hcl', 'nix',
  'dockerfile', 'containerfile', 'service', 'sql', 'prisma', 'liquid',
  // Web 资源
  'html', 'htm', 'xhtml', 'css', 'scss', 'sass', 'less', 'map',
]);

export const TEXT_FILE_NAMES = new Set([
  'dockerfile',
  'containerfile',
  'makefile',
  'gnumakefile',
  'cmakelists.txt',
  'rakefile',
  'gemfile',
  'pipfile',
  'procfile',
  'vagrantfile',
  'justfile',
  'brewfile',
  'license',
  'licence',
  'copying',
  'notice',
  'authors',
  'changelog',
  'readme',
  'todo',
  '.gitignore',
  '.gitattributes',
  '.gitmodules',
  '.dockerignore',
  '.editorconfig',
  '.env',
  '.npmrc',
  '.yarnrc',
  '.babelrc',
  '.eslintrc',
  '.prettierrc',
  '.flake8',
  '.nvmrc',
]);

export const MARKDOWN_EXTENSIONS = new Set(['md', 'markdown', 'mdx']);

/** 取 basename 并转小写（传进来的可能是带目录的路径） */
export function baseName(path) {
  const normalized = String(path || '').replace(/\\/g, '/');
  const slash = normalized.lastIndexOf('/');
  return (slash < 0 ? normalized : normalized.slice(slash + 1)).toLowerCase();
}

/** 小写扩展名（不含点）；`.gitignore` 这类点文件按"无扩展名"处理 */
export function textFileExtension(fileName) {
  const name = baseName(fileName);
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return '';
  return name.slice(dot + 1);
}

export function isTextFileName(fileName) {
  const name = baseName(fileName);
  if (TEXT_FILE_NAMES.has(name)) return true;
  const ext = textFileExtension(name);
  return Boolean(ext) && TEXT_EXTENSIONS.has(ext);
}

/**
 * 是否用文本页打开（与 Dart isTextPreviewable 一致）：
 * 只看"本来没有专门渲染器"的三类，图片 / 音视频 / 文档 / 压缩包各有去处。
 */
export function isTextPreviewable(file) {
  const type = file?.type || 'FILE_TYPE_UNKNOWN';
  if (type !== 'DOCUMENT' && type !== 'APPLICATION' && type !== 'FILE_TYPE_UNKNOWN') return false;
  return isTextFileName(file?.name || '');
}

export function isMarkdownFile(fileName) {
  return MARKDOWN_EXTENSIONS.has(textFileExtension(fileName));
}

function startsWith(bytes, prefix) {
  if (bytes.length < prefix.length) return false;
  for (let i = 0; i < prefix.length; i += 1) {
    if (bytes[i] !== prefix[i]) return false;
  }
  return true;
}

function decodeUtf8(bytes) {
  // fatal:false → 非法字节变成 U+FFFD，与 Dart 的 allowMalformed 一致
  return new TextDecoder('utf-8', { fatal: false }).decode(bytes);
}

function isValidUtf8(bytes) {
  try {
    new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    return true;
  } catch {
    return false;
  }
}

function decodeUtf16(bytes, littleEndian) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const units = [];
  for (let i = 0; i + 1 < bytes.length; i += 2) {
    units.push(view.getUint16(i, littleEndian));
  }
  // 一次 0x8000 个 code unit，避免超长文件爆栈
  let out = '';
  for (let i = 0; i < units.length; i += 0x8000) {
    out += String.fromCharCode.apply(null, units.slice(i, i + 0x8000));
  }
  return out;
}

function normalizeNewlines(text) {
  return text.includes('\r') ? text.replace(/\r\n/g, '\n').replace(/\r/g, '\n') : text;
}

/**
 * 字节 → 可显示文本（BOM → 严格 UTF-8 → 容错 UTF-8）。
 * 返回 {content, truncated, invalidUtf8, bomStripped}
 */
export function decodeTextBytes(bytes, maxBytes = TEXT_MAX_BYTES) {
  const truncated = bytes.length > maxBytes;
  const slice = truncated ? bytes.subarray(0, maxBytes) : bytes;

  let content;
  let invalidUtf8 = false;
  let bomStripped = false;

  if (startsWith(slice, [0xef, 0xbb, 0xbf])) {
    bomStripped = true;
    const body = slice.subarray(3);
    content = decodeUtf8(body);
    invalidUtf8 = !isValidUtf8(body);
  } else if (startsWith(slice, [0xff, 0xfe])) {
    bomStripped = true;
    content = decodeUtf16(slice.subarray(2), true);
  } else if (startsWith(slice, [0xfe, 0xff])) {
    bomStripped = true;
    content = decodeUtf16(slice.subarray(2), false);
  } else {
    invalidUtf8 = !isValidUtf8(slice);
    content = decodeUtf8(slice);
    // 截断可能把一个多字节字符切成两半，这时不算编码有问题
    if (truncated) invalidUtf8 = false;
  }

  return { content: normalizeNewlines(content), truncated, invalidUtf8, bomStripped };
}

// ---------------------------------------------------------------- Markdown

function esc(text) {
  return String(text)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** 链接只允许安全协议（防止 markdown 里塞 javascript:） */
function safeHref(raw) {
  const url = String(raw || '').trim();
  if (/^(https?:|mailto:|#|\/|\.{1,2}\/)/i.test(url)) return esc(url);
  return '#';
}

/** 行内：`code`、**粗**、*斜*、~~删~~、[文](url)、![alt](src) */
function inline(text) {
  let out = esc(text);
  out = out.replace(/`([^`]+)`/g, (_, code) => `<code>${code}</code>`);
  // 链接目标允许一层成对括号（如 javascript:alert(1)），否则会在第一个 ")" 处截断
  const target = String.raw`((?:[^()\s]|\([^()\s]*\))*)`;
  out = out.replace(new RegExp(String.raw`!\[([^\]]*)\]\(${target}\)`, 'g'),
    (_, alt, src) => `<img src="${safeHref(src)}" alt="${alt}" loading="lazy">`);
  out = out.replace(new RegExp(String.raw`\[([^\]]+)\]\(${target}\)`, 'g'),
    (_, label, hrefUrl) => `<a href="${safeHref(hrefUrl)}" target="_blank" rel="noopener noreferrer">${label}</a>`);
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  out = out.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
  out = out.replace(/~~([^~]+)~~/g, '<del>$1</del>');
  return out;
}

function renderTable(rows) {
  const cells = (line) => line.replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
  const head = cells(rows[0]);
  const body = rows.slice(2).map(cells);
  const th = head.map((c) => `<th>${inline(c)}</th>`).join('');
  const trs = body.map((r) => `<tr>${r.map((c) => `<td>${inline(c)}</td>`).join('')}</tr>`).join('');
  return `<table><thead><tr>${th}</tr></thead><tbody>${trs}</tbody></table>`;
}

/**
 * 轻量 Markdown → HTML（够看 README / 笔记，不求完整实现 CommonMark）。
 *
 * 安全前提：所有原文先 esc()，只有本函数生成的标签会进 DOM；链接走 safeHref。
 */
export function renderMarkdown(source) {
  const lines = String(source).split('\n');
  const out = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (/^```/.test(line)) {
      const lang = line.slice(3).trim();
      const buf = [];
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i])) {
        buf.push(lines[i]);
        i += 1;
      }
      i += 1;
      out.push(`<pre><code${lang ? ` class="lang-${esc(lang)}"` : ''}>${esc(buf.join('\n'))}</code></pre>`);
      continue;
    }

    if (/^\s*$/.test(line)) {
      i += 1;
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      i += 1;
      continue;
    }

    if (/^(?:---|\*\*\*|___)\s*$/.test(line)) {
      out.push('<hr>');
      i += 1;
      continue;
    }

    if (/^\s*>/.test(line)) {
      const buf = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*>\s?/, ''));
        i += 1;
      }
      out.push(`<blockquote>${inline(buf.join(' '))}</blockquote>`);
      continue;
    }

    if (/^\s*\|.*\|\s*$/.test(line) && i + 1 < lines.length && /^\s*\|[\s:|-]+\|\s*$/.test(lines[i + 1])) {
      const rows = [];
      while (i < lines.length && /^\s*\|.*\|\s*$/.test(lines[i])) {
        rows.push(lines[i].trim());
        i += 1;
      }
      if (rows.length >= 2) {
        out.push(renderTable(rows));
        continue;
      }
    }

    const listMatch = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(line);
    if (listMatch) {
      const ordered = /\d/.test(listMatch[2]);
      const items = [];
      while (i < lines.length) {
        const m = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(lines[i]);
        if (!m) break;
        items.push(`<li>${inline(m[3])}</li>`);
        i += 1;
      }
      out.push(ordered ? `<ol>${items.join('')}</ol>` : `<ul>${items.join('')}</ul>`);
      continue;
    }

    const buf = [];
    while (i < lines.length && !/^\s*$/.test(lines[i]) &&
      !/^(#{1,6}\s|```|\s*>|\s*[-*+]\s|\s*\d+[.)]\s)/.test(lines[i])) {
      buf.push(lines[i]);
      i += 1;
    }
    if (buf.length) out.push(`<p>${inline(buf.join('\n'))}</p>`);
  }

  return out.join('\n');
}
