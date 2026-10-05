import 'package:flutter/material.dart';
import 'package:flutter_form_builder/flutter_form_builder.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/pages/settings/components/config_field_widget.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/provider.pb.dart';

/// 构造一个 STRING 类型的 ConfigField。
ConfigField _stringField({
  String name = 'token',
  String inputType = '',
  int multilineRows = 0,
  bool required = false,
}) {
  return ConfigField(
    name: name,
    fieldType: ConfigFieldType.STRING,
    label: name,
    required: required,
    inputType: inputType,
    multilineRows: multilineRows,
  );
}

Widget _wrap(Widget child) {
  return MaterialApp(
    home: Scaffold(
      body: FormBuilder(
        child: SingleChildScrollView(child: child),
      ),
    ),
  );
}

void main() {
  group('buildConfigField / STRING', () {
    testWidgets('password + multilineRows=3 不再触发 Flutter 断言', (tester) async {
      // 移动云盘 yun139 的 authorization 字段正是这个组合：
      // InputType=password 且 MultilineRows=3，历史上直接崩掉对话框，
      // 导致「存储提供者类型列表看不到新增项」。
      final field = _stringField(
        name: 'authorization',
        inputType: 'password',
        multilineRows: 3,
        required: true,
      );

      await tester.pumpWidget(_wrap(buildConfigField(field, {})));

      expect(tester.takeException(), isNull);
      expect(find.byType(FormBuilderTextField), findsOneWidget);
    });

    testWidgets('password 字段提供显示/隐藏切换按钮', (tester) async {
      final field = _stringField(name: 'authorization', inputType: 'password');

      await tester.pumpWidget(_wrap(buildConfigField(field, {})));

      // 初始为隐藏态
      expect(find.byIcon(Icons.visibility_off), findsOneWidget);

      await tester.tap(find.byIcon(Icons.visibility_off));
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.visibility), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('隐藏时输入内容，切换为明文后内容保留', (tester) async {
      final field = _stringField(name: 'authorization', inputType: 'password');

      await tester.pumpWidget(_wrap(buildConfigField(field, {})));
      await tester.enterText(find.byType(TextField), 'a-long-token-value');
      await tester.pump();

      await tester.tap(find.byIcon(Icons.visibility_off));
      await tester.pumpAndSettle();

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller?.text, 'a-long-token-value');
      expect(textField.obscureText, isFalse);
      expect(tester.takeException(), isNull);
    });

    testWidgets('非 password 且 multilineRows=3 保持多行', (tester) async {
      final field = _stringField(name: 'note', multilineRows: 3);

      await tester.pumpWidget(_wrap(buildConfigField(field, {})));
      expect(tester.takeException(), isNull);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.maxLines, 3);
      expect(textField.obscureText, isFalse);
    });

    testWidgets('非 password 且未声明 multilineRows 时为单行', (tester) async {
      final field = _stringField(name: 'note');

      await tester.pumpWidget(_wrap(buildConfigField(field, {})));
      expect(tester.takeException(), isNull);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.maxLines, 1);
    });
  });
}