import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:pfm/design/components/components.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/services/services.dart';

/// 日志条目（来源于持久化文件或内存）
class _LogEntry {
  const _LogEntry({
    required this.source,
    required this.message,
    this.time,
    this.levelName,
  });

  /// 'flutter' | 'native'
  final String source;
  final String message;
  final DateTime? time;

  /// 归一化级别: error/warning/info/debug/verbose
  final String? levelName;
}

/// 日志查看页：读取持久化日志（Flutter + 原生）与内存日志，
/// 支持关键词搜索、级别/来源过滤与清空。
class LogPage extends StatefulWidget {
  const LogPage({super.key});

  @override
  State<LogPage> createState() => _LogPageState();
}

class _LogPageState extends State<LogPage> {
  /// 每个日志文件最多解析的尾部行数（防大文件卡 UI）
  static const int _tailLineLimit = 2000;

  final TextEditingController _searchController = TextEditingController();
  String _sourceFilter = 'all';
  String _levelFilter = 'all';
  List<_LogEntry> _entries = const [];
  List<_LogEntry> _filtered = const [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _searchController.addListener(_applyFilter);
    _load();
  }

  @override
  void dispose() {
    _searchController
      ..removeListener(_applyFilter)
      ..dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final entries = <_LogEntry>[];

    // 1. 持久化文件日志（Flutter + 原生）
    final files = await getAllLogFiles();
    for (final f in files) {
      entries.addAll(await _parseFile(f));
    }

    // 2. 内存日志：补充尚未落盘/未轮转的最近记录
    try {
      for (final d in getLogger().history) {
        entries.add(
          _LogEntry(
            source: 'flutter',
            time: d.time,
            levelName: d.logLevel?.name,
            message: d.message?.toString() ?? '',
          ),
        );
      }
    } catch (_) {}

    // 时间倒序；解析失败（无时间）的条目排末尾
    entries.sort((a, b) {
      if (a.time == null && b.time == null) return 0;
      if (a.time == null) return 1;
      if (b.time == null) return -1;
      return b.time!.compareTo(a.time!);
    });

    // 文件与内存的同一条日志去重（时间+级别+消息相同视为重复）
    final seen = <String>{};
    final deduped = <_LogEntry>[];
    for (final e in entries) {
      final key =
          '${e.time?.millisecondsSinceEpoch ?? ''}|${e.levelName}|${e.message}';
      if (seen.add(key)) deduped.add(e);
    }

    if (!mounted) return;
    setState(() {
      _entries = deduped;
      _loading = false;
      _applyFilter();
    });
  }

  /// 解析一个日志文件为条目列表
  Future<List<_LogEntry>> _parseFile(File f) async {
    final isNative = f.path.contains('pfm_native_');
    final fileDate = _fileNameDate(f);
    final lines = await _tailLines(f);
    final result = <_LogEntry>[];
    _LogEntry? pending;

    void flushPending() {
      if (pending != null) result.add(pending!);
      pending = null;
    }

    for (final raw in lines) {
      if (raw.trim().isEmpty) continue;
      final parsed = _parseLine(raw, isNative: isNative, fileDate: fileDate);
      if (parsed != null) {
        flushPending();
        pending = _LogEntry(
          source: isNative ? 'native' : 'flutter',
          time: parsed.$1,
          levelName: parsed.$2,
          message: parsed.$3,
        );
      } else if (pending != null) {
        // 多行日志的续行，归并到上一条
        pending = _LogEntry(
          source: pending!.source,
          time: pending!.time,
          levelName: pending!.levelName,
          message: '${pending!.message}\n$raw',
        );
      }
    }
    flushPending();
    return result;
  }

  /// 解析单行日志，返回 (时间, 级别, 消息)；非日志行返回 null
  (DateTime?, String?, String)? _parseLine(
    String line, {
    required bool isNative,
    required DateTime? fileDate,
  }) {
    if (isNative) {
      final m = RegExp(
        r'^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) \[([IWEDV])\] (.*)$',
      ).firstMatch(line);
      if (m == null) return null;
      return (
        DateTime.tryParse(
          '${m.group(1)!.substring(0, 10)} ${m.group(1)!.substring(11)}',
        ),
        _normLevel(m.group(2)!),
        m.group(3)!,
      );
    }
    final m = RegExp(r'^(\d{2}:\d{2}:\d{2}\.\d{3}) \[([a-zA-Z]+)\](?: (.*))?$')
        .firstMatch(line);
    if (m == null || fileDate == null) return null;
    return (
      DateTime.tryParse(
        '${fileDate.year.toString().padLeft(4, '0')}-'
        '${fileDate.month.toString().padLeft(2, '0')}-'
        '${fileDate.day.toString().padLeft(2, '0')} ${m.group(1)}',
      ),
      _normLevel(m.group(2)!),
      m.group(3) ?? '',
    );
  }

  String _normLevel(String raw) {
    final l = raw.toLowerCase();
    if (l == 'error' || l == 'exception' || l == 'e' || l == 'critical') {
      return 'error';
    }
    if (l == 'warning' || l == 'warn' || l == 'w') return 'warning';
    if (l == 'debug' || l == 'd') return 'debug';
    if (l == 'verbose' || l == 'v') return 'verbose';
    return 'info';
  }

  /// 从文件名提取日期（pfm_2026-08-19.log / pfm_native_20260819.log）
  DateTime? _fileNameDate(File f) {
    final name = f.uri.pathSegments.last;
    final dashed = RegExp(r'(\d{4})-(\d{2})-(\d{2})').firstMatch(name);
    if (dashed != null) {
      return DateTime.tryParse(
        '${dashed.group(1)}-${dashed.group(2)}-${dashed.group(3)}',
      );
    }
    final compact = RegExp(r'(\d{4})(\d{2})(\d{2})').firstMatch(name);
    if (compact != null) {
      return DateTime.tryParse(
        '${compact.group(1)}-${compact.group(2)}-${compact.group(3)}',
      );
    }
    return null;
  }

  /// 从文件尾部读取最多 [_tailLineLimit] 行（大文件不整读）
  Future<List<String>> _tailLines(File f) async {
    try {
      final raf = await f.open();
      try {
        final total = await raf.length();
        if (total <= 0) return const [];
        const maxBytes = 256 * 1024;
        final start = total > maxBytes ? total - maxBytes : 0;
        await raf.setPosition(start);
        final bytes = await raf.read(total - start);
        var lines = utf8.decode(bytes, allowMalformed: true).split('\n');
        if (start > 0) lines.removeAt(0); // 丢弃被块边界截断的首行
        if (lines.length > _tailLineLimit) {
          lines = lines.sublist(lines.length - _tailLineLimit);
        }
        return lines;
      } finally {
        await raf.close();
      }
    } catch (_) {
      return const [];
    }
  }

  void _applyFilter() {
    if (!mounted) return;
    final q = _searchController.text.trim().toLowerCase();
    setState(() {
      _filtered = _entries.where((e) {
        if (_sourceFilter != 'all' && e.source != _sourceFilter) return false;
        if (_levelFilter != 'all' && e.levelName != _levelFilter) {
          return false;
        }
        if (q.isNotEmpty &&
            !e.message.toLowerCase().contains(q) &&
            !(e.levelName ?? '').contains(q)) {
          return false;
        }
        return true;
      }).toList();
    });
  }

  Future<void> _confirmClear() async {
    final al = AppLocalizations.of(context);
    final ok = await PfmDialogs.confirm(
      context,
      title: al.log_clear,
      message: al.log_clearConfirm,
      confirmLabel: al.log_clear,
      isDestructive: true,
    );
    if (!ok || !mounted) return;
    await clearAllLogs();
    if (!mounted) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(al.log_clearDone)));
    await _load();
  }

  // ---------- UI ----------

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final screenWidth = MediaQuery.of(context).size.width;
    final isMobile = screenWidth < PfmConstraints.mobileBreakpoint;

    return Scaffold(
      body: SafeArea(
        child: Column(
          children: [
            Padding(
              padding: EdgeInsets.symmetric(
                horizontal: isMobile ? PfmSpacing.lg : PfmSpacing.xxxxl,
                vertical: PfmSpacing.md,
              ),
              child: Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back, size: 20),
                    onPressed: () => Navigator.pop(context),
                  ),
                  const SizedBox(width: PfmSpacing.sm),
                  Expanded(
                    child: Text(
                      al.log_title,
                      style: PfmTypography.titleLarge,
                    ),
                  ),
                  IconButton(
                    tooltip: al.log_refresh,
                    onPressed: _loading ? null : _load,
                    icon: const Icon(Icons.refresh),
                  ),
                  IconButton(
                    tooltip: al.log_copyAll,
                    onPressed: _loading || _filtered.isEmpty
                        ? null
                        : () => _copyEntries(context, _filtered),
                    icon: const Icon(Icons.copy_all_outlined),
                  ),
                  IconButton(
                    tooltip: al.log_clear,
                    onPressed: _loading ? null : _confirmClear,
                    icon: const Icon(Icons.delete_outline),
                  ),
                ],
              ),
            ),
            _buildSearchBar(al),
            _buildFilterRow(al),
            const Divider(height: 1),
            Expanded(child: _buildList(al)),
          ],
        ),
      ),
    );
  }

  Widget _buildSearchBar(AppLocalizations al) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg),
      child: TextField(
        controller: _searchController,
        decoration: InputDecoration(
          hintText: al.log_searchHint,
          prefixIcon: const Icon(Icons.search, size: 20),
          suffixIcon: _searchController.text.isEmpty
              ? null
              : IconButton(
                  icon: const Icon(Icons.clear, size: 18),
                  onPressed: () => _searchController.clear(),
                ),
          isDense: true,
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(12),
          ),
          contentPadding: const EdgeInsets.symmetric(
            horizontal: 12,
            vertical: 8,
          ),
        ),
      ),
    );
  }

  Widget _buildFilterRow(AppLocalizations al) {
    final sources = <(String, String)>[
      (al.log_sourceAll, 'all'),
      ('Flutter', 'flutter'),
      (al.log_sourceNative, 'native'),
    ];
    final levels = <(String, String)>[
      (al.log_levelAll, 'all'),
      (al.log_levelError, 'error'),
      (al.log_levelWarning, 'warning'),
      (al.log_levelInfo, 'info'),
      (al.log_levelDebug, 'debug'),
      (al.log_levelVerbose, 'verbose'),
    ];
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg),
      child: Row(
        children: [
          for (final (label, value) in sources)
            _filterChip(
              label,
              value,
              _sourceFilter,
              (v) {
                _sourceFilter = v;
                _applyFilter();
              },
            ),
          const SizedBox(width: PfmSpacing.xs),
          for (final (label, value) in levels)
            _filterChip(
              label,
              value,
              _levelFilter,
              (v) {
                _levelFilter = v;
                _applyFilter();
              },
            ),
        ],
      ),
    );
  }

  Widget _filterChip(
    String label,
    String value,
    String current,
    ValueChanged<String> onSelected,
  ) {
    return Padding(
      padding: const EdgeInsets.only(right: 6, bottom: 6),
      child: ChoiceChip(
        label: Text(label),
        selected: current == value,
        onSelected: (_) => onSelected(value),
        visualDensity: VisualDensity.compact,
        labelStyle: PfmTypography.labelSmall.copyWith(
          color: current == value
              ? null
              : Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  Widget _buildList(AppLocalizations al) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_filtered.isEmpty) {
      return Center(
        child: Text(
          al.log_empty,
          style: PfmTypography.bodyMedium.copyWith(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      );
    }
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: PfmSpacing.lg,
            vertical: PfmSpacing.xs,
          ),
          child: Align(
            alignment: Alignment.centerLeft,
            child: Text(
              al.log_count(_filtered.length),
              style: PfmTypography.labelSmall.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ),
        Expanded(
          child: ListView.separated(
            itemCount: _filtered.length,
            separatorBuilder: (_, _) =>
                const Divider(height: 1, indent: PfmSpacing.lg),
            itemBuilder: _buildEntry,
          ),
        ),
      ],
    );
  }

  Widget _buildEntry(BuildContext context, int index) {
    final al = AppLocalizations.of(context);
    final e = _filtered[index];
    final isNative = e.source == 'native';
    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: PfmSpacing.lg,
        vertical: PfmSpacing.sm,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 84,
            child: Text(
              _timeStr(e.time),
              style: PfmTypography.labelSmall.copyWith(
                fontFeatures: const [FontFeature.tabularFigures()],
                color: isNative
                    ? PfmColors.warning
                    : Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          const SizedBox(width: PfmSpacing.xs),
          _levelBadge(e),
          const SizedBox(width: PfmSpacing.sm),
          Expanded(
            child: SelectableText(
              e.message,
              style: PfmTypography.bodyMedium.copyWith(
                fontSize: 12.5,
                height: 1.35,
              ),
              maxLines: 4,
            ),
          ),
          const SizedBox(width: PfmSpacing.xs),
          IconButton(
            tooltip: al.log_copy,
            visualDensity: VisualDensity.compact,
            iconSize: 18,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
            icon: const Icon(Icons.copy_outlined),
            onPressed: () => _copyEntries(context, [e]),
          ),
        ],
      ),
    );
  }

  /// 复制一条/多条日志到剪贴板（带时间与级别前缀）
  Future<void> _copyEntries(
    BuildContext context,
    List<_LogEntry> entries,
  ) async {
    final al = AppLocalizations.of(context);
    final text = entries.map(_formatEntry).join('\n');
    await Clipboard.setData(ClipboardData(text: text));
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(al.log_copyDone(entries.length))),
    );
  }

  String _formatEntry(_LogEntry e) =>
      '${_timeStr(e.time)} [${(e.levelName ?? 'info').toUpperCase()}] '
      '${e.message}';

  Widget _levelBadge(_LogEntry e) {
    final (Color bg, Color fg, String label) = switch (e.levelName) {
      'error' => (PfmColors.errorSurface, PfmColors.error, 'ERR'),
      'warning' => (PfmColors.warningSurface, PfmColors.warning, 'WRN'),
      'debug' => (PfmColors.primarySurface, PfmColors.primary, 'DBG'),
      'verbose' => (Colors.grey.shade200, Colors.grey.shade700, 'VRB'),
      _ => (PfmColors.infoSurface, PfmColors.info, 'INF'),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        label,
        style: PfmTypography.labelSmall.copyWith(
          fontSize: 10,
          fontWeight: FontWeight.w600,
          color: fg,
        ),
      ),
    );
  }

  String _timeStr(DateTime? t) {
    if (t == null) return '--:--:--';
    String two(int n) => n.toString().padLeft(2, '0');
    final ms = t.millisecond.toString().padLeft(3, '0');
    return '${two(t.hour)}:${two(t.minute)}:${two(t.second)}.$ms';
  }
}