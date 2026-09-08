import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/file.pb.dart';

/// 分享管理页 - 查看/复制/编辑/取消我创建的分享链接
class ShareManagePage extends StatefulWidget {
  const ShareManagePage({super.key});

  @override
  State<ShareManagePage> createState() => _ShareManagePageState();
}

class _ShareManagePageState extends State<ShareManagePage> {
  final FmRepository _repo = FmRepository();
  final ScrollController _scrollController = ScrollController();

  List<MyShareItem> _items = [];
  bool _isLoading = true;
  bool _hasMore = false;
  bool _loadingMore = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load(offset: 0);
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent - 200) {
      if (_hasMore && !_loadingMore && !_isLoading) {
        _load(offset: _items.length);
      }
    }
  }

  Future<void> _load({required int offset}) async {
    if (offset == 0) {
      setState(() {
        _isLoading = true;
        _error = null;
      });
    } else {
      setState(() => _loadingMore = true);
    }

    try {
      final result = await _repo.listMyShares(offset: offset, pageSize: 20);
      if (!mounted) return;
      if (result.success && result.data != null) {
        final data = result.data!;
        setState(() {
          if (offset == 0) {
            _items = data.items;
          } else {
            _items.addAll(data.items);
          }
          _hasMore = data.hasMore;
          _isLoading = false;
          _loadingMore = false;
          _error = null;
        });
      } else {
        setState(() {
          _error = result.message ?? '加载失败';
          _isLoading = false;
          _loadingMore = false;
        });
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = '加载失败: $e';
        _isLoading = false;
        _loadingMore = false;
      });
    }
  }

  Future<void> _refresh() => _load(offset: 0);

  // ---------------- 操作 ----------------

  Future<void> _copyLink(MyShareItem item) async {
    final al = AppLocalizations.of(context);
    if (!item.hasShareUrl() || item.shareUrl.isEmpty) return;
    final url = item.shareUrl;
    try {
      await Clipboard.setData(ClipboardData(text: url));
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.shareManage_linkCopied)),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.shareManage_copyFailed)),
        );
      }
    }
  }

  Future<void> _cancelShare(MyShareItem item) async {
    final al = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.shareManage_cancelShare),
        content: Text(al.shareManage_cancelConfirm),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(al.shareManage_cancelShare),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    try {
      final result = await _repo.cancelShareLink(item.shareCode);
      if (!mounted) return;
      if (result.success) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.shareManage_cancelSuccess)),
        );
        await _refresh();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message ?? al.shareManage_cancelFailed)),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.shareManage_cancelFailed}: $e')),
        );
      }
    }
  }

  /// 当前剩余有效期选项：keep=不修改有效期
  String _currentValidityChoice(MyShareItem item) {
    if (item.neverExpire) return 'forever';
    if (item.hasExpireAt()) {
      final remain =
          item.expireAt.seconds.toInt() - _nowSeconds();
      if (remain > 0 && remain <= 7 * 86400) return 'days7';
      if (remain > 0 && remain <= 30 * 86400) return 'days30';
      if (remain > 0) return 'custom';
    }
    return 'keep';
  }

  Future<void> _editShare(MyShareItem item) async {
    final al = AppLocalizations.of(context);

    String selected = _currentValidityChoice(item);
    int initialDays = 7;
    if (item.hasExpireAt()) {
      initialDays =
          ((item.expireAt.seconds.toInt() - _nowSeconds()) / 86400).ceil();
      if (initialDays < 1) initialDays = 1;
    }
    final daysCtrl = TextEditingController(text: '$initialDays');
    final maxCtrl = TextEditingController(
      text: '${item.maxAccessCount}',
    );
    final remarkCtrl = TextEditingController(text: item.remark);

    final saved = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => StatefulBuilder(
        builder: (dialogCtx, setDialogState) => AlertDialog(
          title: Text(al.shareManage_editTitle),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  _fileName(item),
                  style: PfmTypography.bodyMedium,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: PfmSpacing.lg),
                Text(al.shareManage_validity,
                    style: PfmTypography.labelMedium),
                const SizedBox(height: PfmSpacing.sm),
                DropdownButtonFormField<String>(
                  initialValue: selected,
                  decoration: const InputDecoration(isDense: true),
                  items: [
                    for (final c in _validityChoices(al))
                      DropdownMenuItem(
                        value: c,
                        child: Text(_validityLabel(al, c)),
                      ),
                  ],
                  onChanged: (v) {
                    if (v != null) setDialogState(() => selected = v);
                  },
                ),
                if (selected == 'custom') ...[
                  const SizedBox(height: PfmSpacing.sm),
                  TextField(
                    controller: daysCtrl,
                    keyboardType: TextInputType.number,
                    decoration: InputDecoration(
                      labelText: al.shareManage_customDays,
                      isDense: true,
                    ),
                  ),
                ],
                const SizedBox(height: PfmSpacing.lg),
                TextField(
                  controller: maxCtrl,
                  keyboardType: TextInputType.number,
                  decoration: InputDecoration(
                    labelText: al.shareManage_accessLimit,
                    isDense: true,
                  ),
                ),
                const SizedBox(height: PfmSpacing.md),
                TextField(
                  controller: remarkCtrl,
                  decoration: InputDecoration(
                    labelText: al.shareManage_remark,
                    isDense: true,
                  ),
                  maxLength: 100,
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogCtx, false),
              child: Text(al.common_cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialogCtx, true),
              child: Text(al.common_save),
            ),
          ],
        ),
      ),
    );

    if (saved != true || !mounted) return;

    // 组装更新参数
    int? expireSeconds;
    bool? neverExpire;
    switch (selected) {
      case 'forever':
        neverExpire = true;
        break;
      case 'days7':
        expireSeconds = 7 * 86400;
        break;
      case 'days30':
        expireSeconds = 30 * 86400;
        break;
      case 'custom':
        final days = int.tryParse(daysCtrl.text.trim()) ?? 0;
        expireSeconds = days > 0 ? days * 86400 : null;
        break;
      default:
        // keep：保持当前有效期，不传有效期字段
        break;
    }
    final maxCount = int.tryParse(maxCtrl.text.trim());
    final remark = remarkCtrl.text.trim();

    try {
      final result = await _repo.updateShareLink(
        shareCode: item.shareCode,
        expireSeconds: expireSeconds,
        neverExpire: neverExpire,
        maxAccessCount: maxCount,
        remark: remark,
      );
      if (!mounted) return;
      if (result.success) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.shareManage_updateSuccess)),
        );
        // 有效期变更时 sign 已重签，直接复制新链接并提示
        if (result.data != null &&
            result.data!.hasShareUrl() &&
            result.data!.shareUrl.isNotEmpty) {
          await Clipboard.setData(
            ClipboardData(text: result.data!.shareUrl),
          );
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text(al.shareManage_newLinkHint)),
            );
          }
        }
        await _refresh();
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(result.message ?? al.shareManage_updateFailed)),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.shareManage_updateFailed}: $e')),
        );
      }
    }
  }

  int _nowSeconds() => DateTime.now().millisecondsSinceEpoch ~/ 1000;

  List<String> _validityChoices(AppLocalizations al) =>
      ['keep', 'days7', 'days30', 'custom', 'forever'];

  String _validityLabel(AppLocalizations al, String choice) {
    switch (choice) {
      case 'days7':
        return al.shareManage_days7;
      case 'days30':
        return al.shareManage_days30;
      case 'custom':
        return al.shareManage_customDays;
      case 'forever':
        return al.shareManage_forever;
      default:
        return al.shareManage_keepValidity;
    }
  }

  // ---------------- UI ----------------

  bool _shareUsable(MyShareItem item) =>
      item.status == ShareStatus.ACTIVE &&
      !item.fileMissing &&
      !item.fileInTrash;

  /// 过期但文件仍存在的分享可编辑续期（不可复制链接）
  bool _shareEditable(MyShareItem item) =>
      (item.status == ShareStatus.ACTIVE ||
          item.status == ShareStatus.EXPIRED) &&
      !item.fileMissing &&
      !item.fileInTrash;

  String _fileName(MyShareItem item) {
    if (item.hasFileName() && item.fileName.isNotEmpty) return item.fileName;
    if (item.hasFileId()) return item.fileId;
    return 'unknown';
  }

  IconData _fileIcon(FileType type) {
    switch (type) {
      case FileType.FOLDER:
        return Icons.folder;
      case FileType.IMAGE:
        return Icons.image;
      case FileType.VIDEO:
        return Icons.video_file;
      case FileType.AUDIO:
        return Icons.audio_file;
      case FileType.DOCUMENT:
        return Icons.description;
      case FileType.ARCHIVE:
        return Icons.archive;
      default:
        return Icons.insert_drive_file;
    }
  }

  (Color, Color) _fileIconColors(FileType type) {
    switch (type) {
      case FileType.FOLDER:
        return (PfmColors.folder, PfmColors.folderSurface);
      case FileType.IMAGE:
        return (PfmColors.image, PfmColors.imageSurface);
      case FileType.VIDEO:
        return (PfmColors.video, PfmColors.videoSurface);
      case FileType.AUDIO:
        return (PfmColors.image, PfmColors.imageSurface);
      case FileType.DOCUMENT:
        return (PfmColors.document, PfmColors.documentSurface);
      case FileType.ARCHIVE:
        return (PfmColors.archive, PfmColors.archiveSurface);
      default:
        return (PfmColors.lightTextSecondary, PfmColors.lightSurfaceHover);
    }
  }

  String _fmtDate(int seconds) {
    if (seconds <= 0) return '';
    final d = DateTime.fromMillisecondsSinceEpoch(seconds * 1000);
    String two(int v) => v.toString().padLeft(2, '0');
    return '${d.year}-${two(d.month)}-${two(d.day)} ${two(d.hour)}:${two(d.minute)}';
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    return Scaffold(
      appBar: AppBar(title: Text(al.shareManage_title)),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _items.isEmpty
              ? _buildError(al)
              : RefreshIndicator(
                  onRefresh: _refresh,
                  child: _items.isEmpty
                      ? _buildEmpty(al)
                      : ListView.separated(
                          controller: _scrollController,
                          physics: const AlwaysScrollableScrollPhysics(),
                          padding: const EdgeInsets.all(PfmSpacing.lg),
                          itemCount: _items.length + (_hasMore ? 1 : 0),
                          separatorBuilder: (_, _) =>
                              const SizedBox(height: PfmSpacing.sm),
                          itemBuilder: (context, index) {
                            if (index >= _items.length) {
                              return const Padding(
                                padding: EdgeInsets.all(PfmSpacing.lg),
                                child: Center(
                                  child: SizedBox(
                                    width: 24,
                                    height: 24,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  ),
                                ),
                              );
                            }
                            return _buildItem(
                              context,
                              al,
                              colorScheme,
                              _items[index],
                            );
                          },
                        ),
                ),
    );
  }

  Widget _buildError(AppLocalizations al) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(_error ?? al.shareManage_loadFailed),
          const SizedBox(height: PfmSpacing.md),
          FilledButton.tonal(
            onPressed: _refresh,
            child: Text(al.common_retry),
          ),
        ],
      ),
    );
  }

  Widget _buildEmpty(AppLocalizations al) {
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        const SizedBox(height: 120),
        Icon(
          Icons.link_off,
          size: 64,
          color: Theme.of(context).colorScheme.outline,
        ),
        const SizedBox(height: PfmSpacing.lg),
        Center(
          child: Text(
            al.shareManage_empty,
            textAlign: TextAlign.center,
            style: PfmTypography.bodyMedium.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildItem(
    BuildContext context,
    AppLocalizations al,
    ColorScheme colorScheme,
    MyShareItem item,
  ) {
    final (fg, bg) = _fileIconColors(item.fileType);
    final usable = _shareUsable(item);
    final editable = _shareEditable(item);

    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(PfmSpacing.md),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: bg,
                borderRadius: BorderRadius.circular(PfmRadius.sm),
              ),
              child: Icon(_fileIcon(item.fileType), color: fg, size: 22),
            ),
            const SizedBox(width: PfmSpacing.md),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          _fileName(item),
                          style: PfmTypography.bodyMedium.copyWith(
                            fontWeight: FontWeight.w500,
                          ),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      const SizedBox(width: PfmSpacing.sm),
                      _statusChip(al, colorScheme, item),
                    ],
                  ),
                  const SizedBox(height: PfmSpacing.xs),
                  Text(
                    _subtitle(al, item),
                    style: PfmTypography.labelSmall.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                  ),
                  if (item.hasRemark() && item.remark.isNotEmpty) ...[
                    const SizedBox(height: PfmSpacing.xs),
                    Text(
                      item.remark,
                      style: PfmTypography.labelSmall.copyWith(
                        color: colorScheme.onSurfaceVariant,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                  const SizedBox(height: PfmSpacing.sm),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      if (usable) ...[
                        _actionBtn(al, al.shareManage_copyLink, Icons.copy,
                            () => _copyLink(item)),
                      ],
                      if (editable) ...[
                        const SizedBox(width: PfmSpacing.sm),
                        _actionBtn(al, al.shareManage_edit,
                            Icons.edit_outlined, () => _editShare(item)),
                      ],
                      if (item.status == ShareStatus.ACTIVE) ...[
                        const SizedBox(width: PfmSpacing.sm),
                        _actionBtn(al, al.shareManage_cancelShare,
                            Icons.link_off, () => _cancelShare(item),
                            danger: true),
                      ],
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _statusChip(
    AppLocalizations al,
    ColorScheme colorScheme,
    MyShareItem item,
  ) {
    final String label;
    final Color fg;
    final Color bg;
    if (item.fileMissing) {
      label = al.shareManage_fileMissing;
      fg = colorScheme.error;
      bg = colorScheme.errorContainer;
    } else if (item.fileInTrash) {
      label = al.shareManage_fileInTrash;
      fg = colorScheme.error;
      bg = colorScheme.errorContainer;
    } else {
      switch (item.status) {
        case ShareStatus.ACTIVE:
          label = al.shareManage_active;
          fg = PfmColors.success;
          bg = PfmColors.successSurface;
        case ShareStatus.CANCELLED:
          label = al.shareManage_cancelled;
          fg = colorScheme.onSurfaceVariant;
          bg = colorScheme.surfaceContainerHighest;
        case ShareStatus.EXPIRED:
          label = al.shareManage_expired;
          fg = colorScheme.onSurfaceVariant;
          bg = colorScheme.surfaceContainerHighest;
        default:
          label = al.common_unknown;
          fg = colorScheme.onSurfaceVariant;
          bg = colorScheme.surfaceContainerHighest;
      }
    }
    return Container(
      padding:
          const EdgeInsets.symmetric(horizontal: PfmSpacing.sm, vertical: 2),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(PfmRadius.full),
      ),
      child: Text(label, style: PfmTypography.labelSmall.copyWith(color: fg)),
    );
  }

  String _subtitle(AppLocalizations al, MyShareItem item) {
    final parts = <String>[];
    final used = item.accessCount;
    if (item.maxAccessCount > 0) {
      parts.add(al.shareManage_accessUsed(used, item.maxAccessCount));
    } else {
      parts.add(al.shareManage_accessUnlimited(used));
    }
    if (item.neverExpire) {
      parts.add(al.shareManage_forever);
    } else if (item.hasExpireAt() && item.expireAt.seconds > 0) {
      parts.add(
        al.shareManage_expiresAt(_fmtDate(item.expireAt.seconds.toInt())),
      );
    }
    if (item.hasCreatedAt() && item.createdAt.seconds > 0) {
      parts.add(
        al.shareManage_createdAt(_fmtDate(item.createdAt.seconds.toInt())),
      );
    }
    return parts.join(' · ');
  }

  Widget _actionBtn(
    AppLocalizations al,
    String label,
    IconData icon,
    VoidCallback onTap, {
    bool danger = false,
  }) {
    final colorScheme = Theme.of(context).colorScheme;
    return TextButton.icon(
      onPressed: onTap,
      style: TextButton.styleFrom(
        visualDensity: VisualDensity.compact,
        foregroundColor: danger ? colorScheme.error : colorScheme.primary,
        padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.sm),
      ),
      icon: Icon(icon, size: 16),
      label: Text(label, style: PfmTypography.labelMedium),
    );
  }
}
