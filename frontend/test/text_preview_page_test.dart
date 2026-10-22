import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/pages/files/text_preview_page.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';

/// 纯文本预览页：用注入的 loader 顶替网络请求，验证各状态的界面表现。
Widget _wrap(File file, Future<ApiResult<List<int>>> Function(File) loader) {
  return MaterialApp(
    locale: const Locale('zh'),
    localizationsDelegates: AppLocalizations.localizationsDelegates,
    supportedLocales: AppLocalizations.supportedLocales,
    home: TextPreviewPage(file: file, loader: loader),
  );
}

File _file(String name, {FileType type = FileType.APPLICATION}) {
  return File(id: 'f1', name: name, type: type);
}

Future<ApiResult<List<int>>> _ok(String text) async {
  return ApiResult<List<int>>(success: true, data: utf8.encode(text));
}

void main() {
  testWidgets('加载后显示文本内容与文件名', (tester) async {
    await tester.pumpWidget(
      _wrap(_file('config.yaml'), (_) => _ok('server:\n  port: 8080\n')),
    );
    await tester.pumpAndSettle();

    expect(find.text('config.yaml'), findsOneWidget);
    expect(find.textContaining('port: 8080'), findsOneWidget);
  });

  testWidgets('空文件给出提示而不是空白页', (tester) async {
    await tester.pumpWidget(_wrap(_file('empty.txt'), (_) => _ok('   \n\t')));
    await tester.pumpAndSettle();

    expect(find.text('文件内容为空'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.byIcon(Icons.error_outline), findsNothing);
  });

  testWidgets('加载失败显示错误与重试按钮', (tester) async {
    var attempts = 0;
    await tester.pumpWidget(
      _wrap(_file('a.json'), (_) async {
        attempts++;
        return ApiResult<List<int>>(success: false, message: '网络错误');
      }),
    );
    await tester.pumpAndSettle();

    expect(find.text('网络错误'), findsOneWidget);
    expect(find.byIcon(Icons.refresh), findsOneWidget);

    await tester.tap(find.byIcon(Icons.refresh));
    await tester.pumpAndSettle();
    expect(attempts, 2);
  });

  testWidgets('Markdown 默认渲染，可切换回源码', (tester) async {
    await tester.pumpWidget(
      _wrap(
        _file('notes.md', type: FileType.DOCUMENT),
        (_) => _ok('# 标题\n\n正文\n'),
      ),
    );
    await tester.pumpAndSettle();

    // 渲染态：标题不含 `#` 前缀
    expect(find.textContaining('标题'), findsOneWidget);
    expect(find.textContaining('# 标题'), findsNothing);

    await tester.tap(find.byIcon(Icons.code));
    await tester.pumpAndSettle();

    expect(find.textContaining('# 标题'), findsOneWidget);
  });

  testWidgets('非 UTF-8 内容给出编码提示', (tester) async {
    await tester.pumpWidget(
      _wrap(
        _file('legacy.txt', type: FileType.DOCUMENT),
        (_) async =>
            ApiResult<List<int>>(success: true, data: const [0x61, 0xFF, 0x62]),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.info_outline), findsOneWidget);
  });
}
