import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/services/contact_backup_service.dart';

/// 重复联系人预览页。
///
/// 只读展示扫描结果：高置信组默认勾选，存疑组默认不勾并标警告，
/// 不可自动合并的组单列且不可勾选。用户确认后返回选中的组。
class ContactMergePreviewPage extends StatefulWidget {
  const ContactMergePreviewPage({super.key, required this.scan});

  final ContactScanResult scan;

  @override
  State<ContactMergePreviewPage> createState() =>
      _ContactMergePreviewPageState();
}

class _ContactMergePreviewPageState extends State<ContactMergePreviewPage> {
  /// 选中的组下标
  late Set<int> _selected;
  late List<MergePlanEntry> _entries;

  @override
  void initState() {
    super.initState();
    // 排序：可自动合并且不存疑的在前，存疑居中，不可合并的最后
    _entries = [...widget.scan.entries]..sort((a, b) {
      int rank(MergePlanEntry e) {
        if (!e.group.canAutoMerge) return 2;
        return e.group.hasConflict ? 1 : 0;
      }

      final r = rank(a).compareTo(rank(b));
      if (r != 0) return r;
      return b.group.size.compareTo(a.group.size);
    });

    // 默认勾选：可自动合并 且 不存疑
    _selected = {
      for (var i = 0; i < _entries.length; i++)
        if (_entries[i].group.canAutoMerge && !_entries[i].group.hasConflict) i,
    };
  }

  int get _mergeableCount =>
      _entries.where((e) => e.group.canAutoMerge).length;

  int get _selectedCount => _selected.length;

  /// 选中组将删除的联系人总数
  int get _willDeleteCount => _selected.fold(
    0,
    (s, i) => s + _entries[i].merged.deletedIds.length,
  );

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);

    return Scaffold(
      appBar: AppBar(
        title: Text(al.contactMerge_title),
        centerTitle: true,
        actions: [
          if (_mergeableCount > 0)
            TextButton(
              // 全选/取消全选（只作用于可自动合并的组）
              onPressed: () {
                setState(() {
                  if (_selectedCount == _mergeableCount) {
                    _selected = {};
                  } else {
                    _selected = {
                      for (var i = 0; i < _entries.length; i++)
                        if (_entries[i].group.canAutoMerge) i,
                    };
                  }
                });
              },
              child: Text(
                _selectedCount == _mergeableCount
                    ? al.contactMerge_deselectAll
                    : al.contactMerge_selectAll,
              ),
            ),
        ],
      ),
      body: _entries.isEmpty
          ? _buildEmpty(al)
          : ListView(
              padding: const EdgeInsets.only(bottom: 96),
              children: [
                _buildSummary(al),
                for (var i = 0; i < _entries.length; i++) _buildGroupTile(i, al),
              ],
            ),
      bottomNavigationBar: _entries.isEmpty ? null : _buildBottomBar(al),
    );
  }

  Widget _buildEmpty(AppLocalizations al) {
    final cs = Theme.of(context).colorScheme;
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.check_circle_outline, size: 64, color: PfmColors.success),
          const SizedBox(height: 16),
          Text(al.contactMerge_noDuplicates, style: PfmTypography.titleMedium),
          const SizedBox(height: 8),
          Text(
            al.contactMerge_scannedCount(widget.scan.totalContacts),
            style: PfmTypography.bodySmall.copyWith(
              color: cs.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSummary(AppLocalizations al) {
    final cs = Theme.of(context).colorScheme;
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.content_copy, color: PfmColors.primary),
                const SizedBox(width: 8),
                Text(
                  al.contactMerge_foundGroups(widget.scan.entries.length),
                  style: PfmTypography.titleMedium.copyWith(
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            _statRow(
              cs,
              Icons.merge_type,
              al.contactMerge_statMergeable,
              '${widget.scan.autoMergeableCount}',
              PfmColors.primary,
            ),
            if (widget.scan.suspiciousCount > 0)
              _statRow(
                cs,
                Icons.help_outline,
                al.contactMerge_statSuspicious,
                '${widget.scan.suspiciousCount}',
                PfmColors.warning,
              ),
            if (widget.scan.blockedCount > 0)
              _statRow(
                cs,
                Icons.block,
                al.contactMerge_statBlocked,
                '${widget.scan.blockedCount}',
                PfmColors.error,
              ),
            const SizedBox(height: 8),
            Text(
              al.contactMerge_scannedCount(widget.scan.totalContacts),
              style: PfmTypography.bodySmall.copyWith(
                color: cs.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              al.contactMerge_suspiciousHint,
              style: PfmTypography.bodySmall.copyWith(
                color: cs.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _statRow(
    ColorScheme cs,
    IconData icon,
    String label,
    String value,
    Color color,
  ) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        children: [
          Icon(icon, size: 18, color: color),
          const SizedBox(width: 8),
          Expanded(child: Text(label, style: PfmTypography.bodyMedium)),
          Text(
            value,
            style: PfmTypography.bodyMedium.copyWith(
              fontWeight: FontWeight.bold,
              color: color,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildGroupTile(int index, AppLocalizations al) {
    final entry = _entries[index];
    final g = entry.group;
    final cs = Theme.of(context).colorScheme;
    final blocked = !g.canAutoMerge;
    final checked = _selected.contains(index);

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      color: blocked ? cs.errorContainer.withValues(alpha: 0.25) : null,
      child: InkWell(
        // 不可自动合并的组不可勾选
        onTap: blocked
            ? null
            : () => setState(() {
                if (checked) {
                  _selected.remove(index);
                } else {
                  _selected.add(index);
                }
              }),
        borderRadius: BorderRadius.circular(12),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (!blocked)
                    Checkbox(
                      value: checked,
                      onChanged: (_) => setState(() {
                        if (checked) {
                          _selected.remove(index);
                        } else {
                          _selected.add(index);
                        }
                      }),
                    )
                  else
                    Padding(
                      padding: const EdgeInsets.fromLTRB(12, 12, 12, 0),
                      child: Icon(Icons.block, size: 20, color: PfmColors.error),
                    ),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // 合并前 → 合并后
                        _buildBeforeAfter(entry, cs, blocked),
                        const SizedBox(height: 6),
                        // 命中原因 + 成员数
                        Wrap(
                          spacing: 6,
                          runSpacing: 4,
                          children: [
                            _chip(
                              cs,
                              al.contactMerge_memberCount(g.size),
                              cs.onSurfaceVariant,
                            ),
                            for (final r in g.reason)
                              _chip(cs, r, PfmColors.primary),
                          ],
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              // 存疑 / 阻止原因
              if (blocked)
                _warnBox(cs, entry.group.mergeBlockedReason!, PfmColors.error)
              else if (g.hasConflict)
                _warnBox(cs, g.conflictReason ?? '', PfmColors.warning),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildBeforeAfter(
    MergePlanEntry entry,
    ColorScheme cs,
    bool blocked,
  ) {
    final al = AppLocalizations.of(context);
    final before = entry.beforeNames.join('  /  ');
    final after = entry.merged.name;
    final phoneInfo = entry.beforePhoneCount == entry.merged.phones.length
        ? '${entry.merged.phones.length}'
        : '${entry.beforePhoneCount} → ${entry.merged.phones.length}';

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          before,
          style: PfmTypography.bodyMedium.copyWith(
            color: cs.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 2),
        Row(
          children: [
            Icon(
              Icons.arrow_downward,
              size: 14,
              color: blocked ? PfmColors.error : PfmColors.success,
            ),
            const SizedBox(width: 4),
            Expanded(
              child: Text(
                after,
                style: PfmTypography.bodyMedium.copyWith(
                  fontWeight: FontWeight.bold,
                  color: blocked ? PfmColors.error : PfmColors.success,
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 2),
        Text(
          al.contactMerge_phoneSummary(phoneInfo),
          style: PfmTypography.bodySmall.copyWith(color: cs.onSurfaceVariant),
        ),
      ],
    );
  }

  Widget _chip(ColorScheme cs, String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        text,
        style: PfmTypography.bodySmall.copyWith(color: color, fontSize: 11),
      ),
    );
  }

  Widget _warnBox(ColorScheme cs, String text, Color color) {
    if (text.isEmpty) return const SizedBox.shrink();
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.warning_amber_rounded, size: 16, color: color),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              text,
              style: PfmTypography.bodySmall.copyWith(color: color),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBottomBar(AppLocalizations al) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    al.contactMerge_selectedCount(_selectedCount),
                    style: PfmTypography.bodyMedium.copyWith(
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  if (_selectedCount > 0)
                    Text(
                      al.contactMerge_willDelete(_willDeleteCount),
                      style: PfmTypography.bodySmall.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            FilledButton.icon(
              onPressed: _selectedCount == 0
                  ? null
                  // 必须是 List：路由类型为 List<MergePlanEntry>，
                  // 用集合字面量 {...} 会得到 Set，pop 时抛
                  // "_Set<MergePlanEntry> is not a subtype of List<MergePlanEntry>"
                  : () => Navigator.pop<List<MergePlanEntry>>(
                      context,
                      // 按列表顺序返回，保证与页面展示顺序一致
                      [
                        for (var i = 0; i < _entries.length; i++)
                          if (_selected.contains(i)) _entries[i],
                      ],
                    ),
              icon: const Icon(Icons.merge_type),
              label: Text(al.contactMerge_execute),
            ),
          ],
        ),
      ),
    );
  }
}
