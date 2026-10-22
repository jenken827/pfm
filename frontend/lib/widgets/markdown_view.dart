import 'package:flutter/material.dart';
import 'package:markdown/markdown.dart' as md;

/// 轻量 Markdown 渲染（纯 Dart）。
///
/// 只覆盖文档预览会遇到的常用语法：标题、段落、代码块、引用、列表、分隔线、
/// 表格、行内强调/代码/链接。不追求完整 CommonMark，也不渲染 HTML 与图片
/// （图片在文本预览里只显示占位符，避免额外发起请求）。
class MarkdownView extends StatelessWidget {
  const MarkdownView({super.key, required this.source, this.onLinkTap});

  final String source;

  /// 点击链接的回调；为 null 时链接不可点
  final ValueChanged<Uri>? onLinkTap;

  @override
  Widget build(BuildContext context) {
    final nodes = md.Document(
      extensionSet: md.ExtensionSet.gitHubFlavored,
      encodeHtml: false,
    ).parse(source);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: _buildBlocks(context, nodes),
    );
  }

  List<Widget> _buildBlocks(BuildContext context, List<md.Node> nodes) {
    final widgets = <Widget>[];
    for (final node in nodes) {
      final widget = _buildBlock(context, node);
      if (widget != null) widgets.add(widget);
    }
    return widgets;
  }

  Widget? _buildBlock(BuildContext context, md.Node node) {
    if (node is md.Text) {
      if (node.text.trim().isEmpty) return null;
      return _paragraph(context, [node]);
    }
    if (node is! md.Element) return null;

    switch (node.tag) {
      case 'h1':
      case 'h2':
      case 'h3':
      case 'h4':
      case 'h5':
      case 'h6':
        return _heading(context, node);
      case 'p':
        return _paragraph(context, node.children ?? const []);
      case 'pre':
        return _codeBlock(context, node);
      case 'blockquote':
        return _blockquote(context, node);
      case 'ul':
      case 'ol':
        return _list(context, node, 0);
      case 'hr':
        return const Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Divider(height: 1),
        );
      case 'table':
        return _table(context, node);
      default:
        // 其余容器（div / section 等）把子节点摊平
        final children = _buildBlocks(context, node.children ?? const []);
        if (children.isEmpty) return null;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: children,
        );
    }
  }

  Widget _heading(BuildContext context, md.Element node) {
    final level = int.tryParse(node.tag.substring(1)) ?? 1;
    final theme = Theme.of(context);
    final style = switch (level) {
      1 => theme.textTheme.headlineSmall,
      2 => theme.textTheme.titleLarge,
      3 => theme.textTheme.titleMedium,
      _ => theme.textTheme.titleSmall,
    };
    return Padding(
      padding: EdgeInsets.only(top: level == 1 ? 4 : 16, bottom: 6),
      child: SelectableText.rich(
        TextSpan(
          children: _buildInline(context, node.children ?? const []),
          style: style?.copyWith(fontWeight: FontWeight.w600),
        ),
      ),
    );
  }

  Widget _paragraph(BuildContext context, List<md.Node> children) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: SelectableText.rich(
        TextSpan(children: _buildInline(context, children)),
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(height: 1.6),
      ),
    );
  }

  Widget _codeBlock(BuildContext context, md.Element node) {
    final code = node.children?.whereType<md.Element>().firstWhere(
      (e) => e.tag == 'code',
      orElse: () => md.Element.text('code', node.textContent),
    );
    final language = _codeLanguage(code?.attributes['class']);
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.symmetric(vertical: 8),
      decoration: BoxDecoration(
        color: colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (language != null)
            Padding(
              padding: const EdgeInsets.only(left: 12, top: 8),
              child: Text(
                language,
                style: TextStyle(
                  fontSize: 11,
                  color: colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.all(12),
            child: SelectableText(
              (code?.textContent ?? '').trimRight(),
              style: const TextStyle(
                fontFamily: 'monospace',
                fontSize: 13,
                height: 1.5,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _blockquote(BuildContext context, md.Element node) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 8),
      padding: const EdgeInsets.only(left: 12),
      decoration: BoxDecoration(
        border: Border(
          left: BorderSide(color: colorScheme.outlineVariant, width: 3),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: _buildBlocks(context, node.children ?? const []),
      ),
    );
  }

  Widget _list(BuildContext context, md.Element node, int depth) {
    final ordered = node.tag == 'ol';
    final start = int.tryParse(node.attributes['start'] ?? '1') ?? 1;
    final items = (node.children ?? const []).whereType<md.Element>().toList();
    return Padding(
      padding: EdgeInsets.only(left: depth == 0 ? 0 : 16, top: 2, bottom: 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          for (var i = 0; i < items.length; i++)
            Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(
                    width: 26,
                    child: Text(
                      ordered ? '${start + i}.' : '•',
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                  ),
                  Expanded(child: _listItemBody(context, items[i], depth)),
                ],
              ),
            ),
        ],
      ),
    );
  }

  /// 列表项内容：纯文本项走行内富文本；带嵌套列表 / 多段落的项逐个块渲染
  Widget _listItemBody(BuildContext context, md.Element item, int depth) {
    final blocks = item.children ?? const <md.Node>[];
    final hasNested = blocks.whereType<md.Element>().any(
      (e) => e.tag == 'ul' || e.tag == 'ol' || e.tag == 'pre' || e.tag == 'p',
    );
    if (!hasNested) {
      return SelectableText.rich(
        TextSpan(children: _buildInline(context, blocks)),
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(height: 1.5),
      );
    }
    final children = <Widget>[];
    for (final child in blocks) {
      if (child is md.Element && (child.tag == 'ul' || child.tag == 'ol')) {
        children.add(_list(context, child, depth + 1));
        continue;
      }
      final widget = _buildBlock(context, child);
      if (widget != null) children.add(widget);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: children,
    );
  }

  Widget _table(BuildContext context, md.Element node) {
    final rows = <TableRow>[];
    for (final section in (node.children ?? const []).whereType<md.Element>()) {
      for (final row
          in (section.children ?? const []).whereType<md.Element>()) {
        final cells = <Widget>[];
        for (final cell in (row.children ?? const []).whereType<md.Element>()) {
          cells.add(_tableCell(context, cell));
        }
        if (cells.isNotEmpty) rows.add(TableRow(children: cells));
      }
    }
    if (rows.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Table(
        border: TableBorder.all(
          color: Theme.of(context).colorScheme.outlineVariant,
        ),
        defaultColumnWidth: const IntrinsicColumnWidth(),
        children: rows,
      ),
    );
  }

  Widget _tableCell(BuildContext context, md.Element cell) {
    final isHeader = cell.tag == 'th';
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      child: SelectableText.rich(
        TextSpan(children: _buildInline(context, cell.children ?? const [])),
        style: Theme.of(context).textTheme.bodySmall?.copyWith(
          fontWeight: isHeader ? FontWeight.w600 : FontWeight.normal,
        ),
      ),
    );
  }

  List<InlineSpan> _buildInline(BuildContext context, List<md.Node> nodes) {
    final spans = <InlineSpan>[];
    for (final node in nodes) {
      if (node is md.Text) {
        spans.add(TextSpan(text: node.text));
        continue;
      }
      if (node is! md.Element) continue;
      spans.addAll(_buildInlineElement(context, node));
    }
    return spans;
  }

  List<InlineSpan> _buildInlineElement(BuildContext context, md.Element node) {
    final inner = _buildInline(context, node.children ?? const <md.Node>[]);
    switch (node.tag) {
      case 'strong':
        return [
          TextSpan(
            children: inner,
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
        ];
      case 'em':
        return [
          TextSpan(
            children: inner,
            style: const TextStyle(fontStyle: FontStyle.italic),
          ),
        ];
      case 'del':
        return [
          TextSpan(
            children: inner,
            style: const TextStyle(decoration: TextDecoration.lineThrough),
          ),
        ];
      case 'code':
        return [
          TextSpan(
            text: node.textContent,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 13,
              backgroundColor: Theme.of(context)
                  .colorScheme
                  .surfaceContainerHighest,
            ),
          ),
        ];
      case 'br':
        return const [TextSpan(text: '\n')];
      case 'a':
        return [_linkSpan(context, node, inner)];
      case 'img':
        return [TextSpan(text: '[图片: ${node.attributes['alt'] ?? ''}]')];
      default:
        return inner;
    }
  }

  /// 链接用 [WidgetSpan] 包一层手势：避免为每次 build 创建 TapGestureRecognizer
  InlineSpan _linkSpan(
    BuildContext context,
    md.Element node,
    List<InlineSpan> inner,
  ) {
    final uri = Uri.tryParse(node.attributes['href'] ?? '');
    final color = Theme.of(context).colorScheme.primary;
    final text = Text.rich(
      TextSpan(children: inner),
      style: TextStyle(color: color, decoration: TextDecoration.underline),
    );
    if (uri == null || onLinkTap == null) {
      return WidgetSpan(
        alignment: PlaceholderAlignment.baseline,
        baseline: TextBaseline.alphabetic,
        child: text,
      );
    }
    return WidgetSpan(
      alignment: PlaceholderAlignment.baseline,
      baseline: TextBaseline.alphabetic,
      child: GestureDetector(onTap: () => onLinkTap!(uri), child: text),
    );
  }

  String? _codeLanguage(String? className) {
    if (className == null) return null;
    for (final token in className.split(' ')) {
      if (token.startsWith('language-')) {
        final lang = token.substring('language-'.length);
        if (lang.isNotEmpty) return lang;
      }
    }
    return null;
  }
}
