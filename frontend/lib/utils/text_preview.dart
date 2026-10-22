import 'dart:convert';
import 'dart:typed_data';

import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';

/// 纯文本预览的判定与解码规则。
///
/// 文本类文件（配置、代码、日志、Markdown）走 Dart 原生渲染，不进 Flyfish
/// Viewer：体积小、打开频繁，走 WebView 要拉 JS chunk，得不偿失。
/// 判定只看**文件名**，不看后端 `FileType`：后端把 txt/md 归到 DOCUMENT、
/// 把 json/yaml 归到 APPLICATION，粒度都不是"能不能按文本显示"。

/// 单次预览最多读取的字节数，超过则截断（避免把几 MB 的日志整段塞进布局）
const int kTextPreviewMaxBytes = 1024 * 1024;

/// 可按纯文本预览的扩展名（小写，不含点）
const Set<String> _textExtensions = {
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
};

/// 没有扩展名、但约定俗成是纯文本的文件名（小写比较）
const Set<String> _textFileNames = {
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
};

/// Markdown 扩展名（支持"渲染 / 源码"切换）
const Set<String> _markdownExtensions = {'md', 'markdown', 'mdx'};

/// 取小写扩展名（不含点），无扩展名返回空串
///
/// 传入的可能是带目录的路径，先取 basename（`a/b/Makefile` 要按文件名判断）
String textFileExtension(String fileName) {
  final name = _baseName(fileName);
  final dot = name.lastIndexOf('.');
  // 形如 `.gitignore` 的点文件没有扩展名，走文件名白名单
  if (dot <= 0 || dot == name.length - 1) return '';
  return name.substring(dot + 1);
}

/// 文件名是否属于可按纯文本预览的类型
bool isTextFileName(String fileName) {
  final name = _baseName(fileName);
  if (_textFileNames.contains(name)) return true;
  final ext = textFileExtension(name);
  return ext.isNotEmpty && _textExtensions.contains(ext);
}

/// 该文件是否应该用 Dart 文本预览（而不是 WebView / 详情页）
///
/// 只对"本来就没有专门渲染器"的类型生效：图片、音视频有原生预览，压缩包和
/// Office 文档交给 Flyfish Viewer。
bool isTextPreviewable(File file) {
  switch (file.type) {
    case FileType.DOCUMENT:
    case FileType.APPLICATION:
    case FileType.FILE_TYPE_UNKNOWN:
      return isTextFileName(file.name);
    default:
      return false;
  }
}

/// 是否是 Markdown（决定默认显示渲染结果还是源码）
bool isMarkdownFile(String fileName) {
  return _markdownExtensions.contains(textFileExtension(fileName));
}

/// 文本解码结果
class DecodedText {
  const DecodedText({
    required this.content,
    this.truncated = false,
    this.invalidUtf8 = false,
    this.bomStripped = false,
  });

  /// 解码后的正文（已把 CRLF / CR 归一成 LF）
  final String content;

  /// 是否因为超出 [kTextPreviewMaxBytes] 被截断
  final bool truncated;

  /// UTF-8 非法字节被替换成了 U+FFFD（多半不是 UTF-8 编码）
  final bool invalidUtf8;

  /// 是否带 BOM（UTF-8 / UTF-16）
  final bool bomStripped;

  bool get isEmpty => content.trim().isEmpty;
}

/// 把文件字节解码成可显示文本。
///
/// 顺序：BOM（UTF-8 / UTF-16LE / UTF-16BE）→ 严格 UTF-8 → 容错 UTF-8。
/// 非 UTF-8 的本地编码（如 GBK）会得到替换字符，界面据此提示用户。
DecodedText decodeTextBytes(
  Uint8List bytes, {
  int maxBytes = kTextPreviewMaxBytes,
}) {
  final truncated = bytes.length > maxBytes;
  final slice = truncated ? bytes.sublist(0, maxBytes) : bytes;

  String content;
  var invalidUtf8 = false;
  var bomStripped = false;

  if (_startsWith(slice, const [0xEF, 0xBB, 0xBF])) {
    bomStripped = true;
    final body = slice.sublist(3);
    content = _decodeUtf8(body);
    invalidUtf8 = !_isValidUtf8(body);
  } else if (_startsWith(slice, const [0xFF, 0xFE])) {
    bomStripped = true;
    content = _decodeUtf16(slice.sublist(2), littleEndian: true);
  } else if (_startsWith(slice, const [0xFE, 0xFF])) {
    bomStripped = true;
    content = _decodeUtf16(slice.sublist(2), littleEndian: false);
  } else {
    invalidUtf8 = !_isValidUtf8(slice);
    content = _decodeUtf8(slice);
    // 截断可能把一个多字节字符切成两半，这时不算"编码有问题"
    if (truncated) invalidUtf8 = false;
  }

  return DecodedText(
    content: _normalizeNewlines(content),
    truncated: truncated,
    invalidUtf8: invalidUtf8,
    bomStripped: bomStripped,
  );
}

/// 字节长度的人话表示（文本预览的"文件过大"提示用）
String formatTextSizeHint(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
}

String _baseName(String path) {
  final normalized = path.replaceAll('\\', '/');
  final slash = normalized.lastIndexOf('/');
  return (slash < 0 ? normalized : normalized.substring(slash + 1))
      .toLowerCase();
}

bool _startsWith(Uint8List bytes, List<int> prefix) {
  if (bytes.length < prefix.length) return false;
  for (var i = 0; i < prefix.length; i++) {
    if (bytes[i] != prefix[i]) return false;
  }
  return true;
}

bool _isValidUtf8(List<int> bytes) {
  try {
    utf8.decode(bytes);
    return true;
  } on FormatException {
    return false;
  }
}

String _decodeUtf8(List<int> bytes) => utf8.decode(bytes, allowMalformed: true);

/// UTF-16 解码（Dart 没有内置 codec，这里按 code unit 组装）；
/// 代理对（surrogate pair，emoji 等）由 [String.fromCharCodes] 自动合并。
String _decodeUtf16(List<int> bytes, {required bool littleEndian}) {
  final units = <int>[];
  for (var i = 0; i + 1 < bytes.length; i += 2) {
    units.add(
      littleEndian
          ? bytes[i] | (bytes[i + 1] << 8)
          : (bytes[i] << 8) | bytes[i + 1],
    );
  }
  return String.fromCharCodes(units);
}

String _normalizeNewlines(String text) {
  if (!text.contains('\r')) return text;
  return text.replaceAll('\r\n', '\n').replaceAll('\r', '\n');
}
