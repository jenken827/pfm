import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/utils/text_preview.dart';

File _file(String name, FileType type) => File(name: name, type: type);

Uint8List _bytes(List<int> data) => Uint8List.fromList(data);

void main() {
  group('isTextFileName', () {
    test('配置 / 代码 / 日志按扩展名识别', () {
      for (final name in [
        'config.yaml',
        'app.toml',
        'package.json',
        'main.dart',
        'server.go',
        'run.sh',
        'notes.md',
        'app.log',
        'data.csv',
        'index.html',
        'a/b/Dockerfile',
      ]) {
        expect(isTextFileName(name), isTrue, reason: name);
      }
    });

    test('大写扩展名同样识别', () {
      expect(isTextFileName('README.MD'), isTrue);
      expect(isTextFileName('CONFIG.YAML'), isTrue);
    });

    test('图片 / 视频 / 压缩包 / Office 不算文本', () {
      for (final name in [
        'photo.jpg',
        'clip.mp4',
        'song.mp3',
        'bundle.zip',
        'report.docx',
        'sheet.xlsx',
        'slides.pptx',
        'manual.pdf',
      ]) {
        expect(isTextFileName(name), isFalse, reason: name);
      }
    });

    test('无扩展名走文件名白名单', () {
      expect(isTextFileName('Makefile'), isTrue);
      expect(isTextFileName('Dockerfile'), isTrue);
      expect(isTextFileName('LICENSE'), isTrue);
      expect(isTextFileName('.gitignore'), isTrue);
      expect(isTextFileName('README'), isTrue);
      expect(isTextFileName('somebinary'), isFalse);
    });

    test('点文件不会被当成扩展名', () {
      // `.env` 属于白名单文件，但 `.unknown` 这种不能按 "unknown 扩展名" 处理
      expect(isTextFileName('.env'), isTrue);
      expect(isTextFileName('.unknown'), isFalse);
    });
  });

  group('isTextPreviewable', () {
    test('DOCUMENT 里的纯文本可预览，PDF/Office 不可', () {
      expect(isTextPreviewable(_file('a.txt', FileType.DOCUMENT)), isTrue);
      expect(isTextPreviewable(_file('a.md', FileType.DOCUMENT)), isTrue);
      expect(isTextPreviewable(_file('a.pdf', FileType.DOCUMENT)), isFalse);
      expect(isTextPreviewable(_file('a.docx', FileType.DOCUMENT)), isFalse);
    });

    test('后端归到 APPLICATION 的 json/yaml 也能预览', () {
      expect(isTextPreviewable(_file('a.json', FileType.APPLICATION)), isTrue);
      expect(isTextPreviewable(_file('a.yaml', FileType.APPLICATION)), isTrue);
      expect(isTextPreviewable(_file('a.exe', FileType.APPLICATION)), isFalse);
    });

    test('图片 / 压缩包交给别的预览器', () {
      expect(isTextPreviewable(_file('a.png', FileType.IMAGE)), isFalse);
      expect(isTextPreviewable(_file('a.zip', FileType.ARCHIVE)), isFalse);
    });
  });

  test('isMarkdownFile 只认 Markdown 扩展名', () {
    expect(isMarkdownFile('a.md'), isTrue);
    expect(isMarkdownFile('a.markdown'), isTrue);
    expect(isMarkdownFile('a.MDX'), isTrue);
    expect(isMarkdownFile('a.txt'), isFalse);
  });

  group('decodeTextBytes', () {
    test('UTF-8 正常解码', () {
      final decoded = decodeTextBytes(_bytes(utf8.encode('中文内容\nsecond')));
      expect(decoded.content, '中文内容\nsecond');
      expect(decoded.invalidUtf8, isFalse);
      expect(decoded.truncated, isFalse);
      expect(decoded.bomStripped, isFalse);
    });

    test('UTF-8 BOM 被去掉', () {
      final decoded = decodeTextBytes(
        _bytes([0xEF, 0xBB, 0xBF, ...utf8.encode('hello')]),
      );
      expect(decoded.content, 'hello');
      expect(decoded.bomStripped, isTrue);
    });

    test('UTF-16LE BOM 能解码', () {
      // "你好" = 0x4F60 0x597D
      final decoded = decodeTextBytes(
        _bytes([0xFF, 0xFE, 0x60, 0x4F, 0x7D, 0x59]),
      );
      expect(decoded.content, '你好');
      expect(decoded.bomStripped, isTrue);
    });

    test('UTF-16BE BOM 能解码', () {
      final decoded = decodeTextBytes(
        _bytes([0xFE, 0xFF, 0x4F, 0x60, 0x59, 0x7D]),
      );
      expect(decoded.content, '你好');
    });

    test('非法 UTF-8 标记出来，不抛异常', () {
      // 0xFF 不是合法 UTF-8 起始字节（GBK 的 "中" 就是 0xD6 0xD0）
      final decoded = decodeTextBytes(_bytes([0x61, 0xFF, 0x62]));
      expect(decoded.invalidUtf8, isTrue);
      expect(decoded.content, contains('a'));
      expect(decoded.content, contains('b'));
    });

    test('CRLF / CR 归一成 LF', () {
      final decoded = decodeTextBytes(_bytes(utf8.encode('a\r\nb\rc')));
      expect(decoded.content, 'a\nb\nc');
    });

    test('超过上限时截断并标记', () {
      final payload = utf8.encode('x' * 64);
      final decoded = decodeTextBytes(_bytes(payload), maxBytes: 16);
      expect(decoded.truncated, isTrue);
      expect(decoded.content.length, 16);
    });

    test('空白文件视为空', () {
      expect(decodeTextBytes(_bytes(utf8.encode('  \n\t'))).isEmpty, isTrue);
      expect(decodeTextBytes(_bytes(utf8.encode('a'))).isEmpty, isFalse);
    });
  });

  test('formatTextSizeHint 按量级给单位', () {
    expect(formatTextSizeHint(512), '512 B');
    expect(formatTextSizeHint(2048), '2.0 KB');
    expect(formatTextSizeHint(3 * 1024 * 1024), '3.0 MB');
  });
}
