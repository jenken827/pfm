import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/widgets/markdown_view.dart';

Widget _wrap(String source, {ValueChanged<Uri>? onLinkTap}) {
  return MaterialApp(
    home: Scaffold(
      body: SingleChildScrollView(
        child: MarkdownView(source: source, onLinkTap: onLinkTap),
      ),
    ),
  );
}

void main() {
  testWidgets('标题 / 段落 / 行内强调都能渲染', (tester) async {
    await tester.pumpWidget(_wrap('# 一级标题\n\n正文里有 **粗体** 和 `code`。\n'));

    expect(find.textContaining('一级标题'), findsOneWidget);
    expect(find.textContaining('正文里有'), findsOneWidget);
    expect(find.textContaining('粗体'), findsOneWidget);
    expect(find.textContaining('code'), findsOneWidget);
  });

  testWidgets('代码块渲染语言标签与内容', (tester) async {
    await tester.pumpWidget(_wrap('```dart\nvoid main() {}\n```\n'));

    expect(find.text('dart'), findsOneWidget);
    expect(find.textContaining('void main()'), findsOneWidget);
  });

  testWidgets('无序列表与有序列表渲染符号', (tester) async {
    await tester.pumpWidget(_wrap('- 第一项\n- 第二项\n\n1. 步骤一\n2. 步骤二\n'));

    expect(find.text('•'), findsNWidgets(2));
    expect(find.text('1.'), findsOneWidget);
    expect(find.text('2.'), findsOneWidget);
    expect(find.textContaining('第一项'), findsOneWidget);
    expect(find.textContaining('步骤二'), findsOneWidget);
  });

  testWidgets('表格渲染为 Table', (tester) async {
    await tester.pumpWidget(_wrap('| 名称 | 数量 |\n| --- | --- |\n| 苹果 | 3 |\n'));

    expect(find.byType(Table), findsOneWidget);
    expect(find.textContaining('苹果'), findsOneWidget);
  });

  testWidgets('链接点击回调收到 Uri', (tester) async {
    Uri? tapped;
    await tester.pumpWidget(
      _wrap('[示例](https://example.com/a)', onLinkTap: (uri) => tapped = uri),
    );

    expect(find.text('示例'), findsOneWidget);
    await tester.tap(find.text('示例'));
    expect(tapped, Uri.parse('https://example.com/a'));
  });

  testWidgets('图片只显示占位符，不发起请求', (tester) async {
    await tester.pumpWidget(_wrap('![截图](https://example.com/a.png)'));

    expect(find.textContaining('[图片: 截图]'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
  });

  testWidgets('引用块与分隔线', (tester) async {
    await tester.pumpWidget(_wrap('> 引用内容\n\n---\n'));

    expect(find.textContaining('引用内容'), findsOneWidget);
    expect(find.byType(Divider), findsOneWidget);
  });
}
