import 'package:flutter/material.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/design/components/components.dart';
import 'package:pfm/models/contact_merge_plan.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/pages/settings/contact_merge_preview_page.dart';
import 'package:pfm/services/contact_backup_service.dart';
import 'package:pfm/services/contact_undo_service.dart';
import 'package:pfm/utils/platform.dart';

/// 通讯录备份页面
class ContactBackupPage extends StatefulWidget {
  const ContactBackupPage({super.key});

  @override
  State<ContactBackupPage> createState() => _ContactBackupPageState();
}

class _ContactBackupPageState extends State<ContactBackupPage> {
  late ContactBackupService _backupService;
  ContactBackupResult _result = const ContactBackupResult();
  bool _isLoading = true;
  bool _isScanning = false;
  bool _isRestoring = false;
  UndoPointInfo? _undoInfo;

  @override
  void initState() {
    super.initState();
    _initService();
  }

  Future<void> _initService() async {
    _backupService = await ContactBackupService.instance();
    _result = _backupService.result;

    // 监听状态变化
    _backupService.statusStream.listen((result) {
      if (mounted) {
        setState(() => _result = result);
      }
    });

    await _refreshUndoPoint();
    setState(() => _isLoading = false);
  }

  Future<void> _startBackup() async {
    await _backupService.startBackup();
  }

  /// 扫描重复联系人（只读，不申请写权限）
  Future<void> _scanDuplicates() async {
    final al = AppLocalizations.of(context);
    setState(() => _isScanning = true);
    try {
      final scan = await _backupService.scanDuplicates();
      if (!mounted) return;
      if (scan == null) {
        // 无权限或失败，错误信息已由 service 通过 statusStream 上报
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(_result.errorMessage ?? al.common_failed)),
        );
        return;
      }

      final selected = await Navigator.push<List<MergePlanEntry>>(
        context,
        MaterialPageRoute(
          builder: (_) => ContactMergePreviewPage(scan: scan),
        ),
      );
      if (!mounted || selected == null || selected.isEmpty) return;

      // 二次确认（破坏性操作，明确告知影响面）
      final groups = selected.length;
      final willDelete = selected.fold<int>(
        0,
        (s, e) => s + e.merged.deletedIds.length,
      );
      final confirmed = await PfmDialogs.confirm(
        context,
        title: al.contactMerge_confirmTitle,
        message: al.contactMerge_confirmMsg(groups, willDelete),
        confirmLabel: al.contactMerge_confirmOk,
        isDestructive: true,
      );
      if (!confirmed || !mounted) return;

      final outcome = await _backupService.executeMerge(selected);
      if (!mounted) return;

      // 删除被系统拦截：号码已合并，但冗余记录还在。
      // 用弹窗（而非 snackbar）提示，因为需要用户去系统设置处理。
      if (outcome.deleteBlocked) {
        await _showDeleteBlockedDialog(outcome.blockedDeleteIds.length);
        if (!mounted) return;
      }

      if (outcome.failures.isNotEmpty) {
        // 部分失败：如实展示失败条数，并记日志便于排查
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              outcome.mergedGroups > 0
                  ? '${al.contactMerge_done(outcome.mergedGroups, outcome.deletedCount)}；'
                        '${al.contactMerge_partialFailed(outcome.failures.length)}'
                  : (outcome.errorMessage ?? al.common_failed),
            ),
            duration: const Duration(seconds: 6),
          ),
        );
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              al.contactMerge_done(outcome.mergedGroups, outcome.deletedCount),
            ),
          ),
        );
      }
      await _refreshUndoPoint();
    } finally {
      if (mounted) setState(() => _isScanning = false);
    }
  }

  /// 从服务器备份恢复通讯录
  Future<void> _restoreFromBackup() async {
    final al = AppLocalizations.of(context);

    setState(() => _isRestoring = true);
    try {
      // 1) 列备份文件
      final files = await _backupService.listBackupFiles();
      if (!mounted) return;
      if (files == null) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.contactRestore_loadFailed)),
        );
        return;
      }
      if (files.isEmpty) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.contactRestore_noFiles)),
        );
        return;
      }

      // 2) 选一个
      final picked = await _showFilePicker(files);
      if (!mounted || picked == null) return;

      // 3) 下载 + 解析 + 规划（只读）
      // 用 snackbar 提示耗时操作已开始（下载 + 解析可能数秒）
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.contactRestore_downloading)),
      );
      final plan = await _backupService.planRestoreFromBackup(picked.fileId);
      if (!mounted) return;
      if (plan == null) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(_result.errorMessage ?? al.contactRestore_loadFailed),
          ),
        );
        return;
      }

      if (!plan.hasChanges) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.contactRestore_nothingToDo)),
        );
        return;
      }

      // 4) 确认
      final confirmed = await PfmDialogs.confirm(
        context,
        title: al.contactRestore_confirmTitle,
        message: al.contactRestore_confirmMsg(
          picked.name,
          plan.createCount,
          plan.mergeCount,
          plan.skipCount,
        ),
        confirmLabel: al.common_confirm,
        isDestructive: true,
      );
      if (!confirmed || !mounted) return;

      // 5) 执行
      final outcome = await _backupService.executeRestore(plan);
      if (!mounted) return;

      if (outcome.failures.isNotEmpty) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              outcome.failures.length == 1
                  ? outcome.failures.first
                  : '${outcome.failures.first}（共 ${outcome.failures.length} 项失败）',
            ),
            duration: const Duration(seconds: 6),
          ),
        );
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              al.contactRestore_done(
                outcome.restoredCount,
                outcome.mergedGroups,
              ),
            ),
          ),
        );
      }
      await _refreshUndoPoint();
    } finally {
      if (mounted) setState(() => _isRestoring = false);
    }
  }

  /// 备份文件选择弹窗
  Future<BackupFileInfo?> _showFilePicker(List<BackupFileInfo> files) {
    final al = AppLocalizations.of(context);
    return showModalBottomSheet<BackupFileInfo>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const SizedBox(height: 12),
            PfmDialogs.buildDragHandle(ctx),
            const SizedBox(height: 8),
            Text(
              al.contactRestore_pickFile,
              style: PfmTypography.titleMedium.copyWith(
                fontWeight: FontWeight.bold,
              ),
            ),
            const SizedBox(height: 8),
            Flexible(
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: files.length,
                itemBuilder: (_, i) {
                  final f = files[i];
                  return ListTile(
                    leading: const Icon(Icons.description_outlined),
                    title: Text(f.name),
                    subtitle: Text(
                      al.contactRestore_fileInfo(
                        _formatSize(f.size),
                        _formatTime(f.updatedAt),
                      ),
                    ),
                    onTap: () => Navigator.pop(ctx, f),
                  );
                },
              ),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    return '${(bytes / 1024 / 1024).toStringAsFixed(1)} MB';
  }

  String _formatTime(DateTime t) {
    final l = t.toLocal();
    String two(int v) => v.toString().padLeft(2, '0');
    return '${l.year}-${two(l.month)}-${two(l.day)} '
        '${two(l.hour)}:${two(l.minute)}';
  }

  /// 撤销上次合并
  Future<void> _undoLastMerge() async {
    final al = AppLocalizations.of(context);
    final info = await ContactUndoService.info();
    if (!mounted) return;
    if (info == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.contactMerge_undoNone)),
      );
      return;
    }

    final confirmed = await PfmDialogs.confirm(
      context,
      title: al.contactMerge_undoConfirmTitle,
      message: al.contactMerge_undoConfirmMsg(
        info.deletedCount,
        info.modifiedCount,
      ),
      confirmLabel: al.contactMerge_undo,
      isDestructive: true,
    );
    if (!confirmed || !mounted) return;

    setState(() => _isScanning = true);
    try {
      final outcome = await _backupService.undoLastMerge();
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            outcome.success
                ? al.contactMerge_undoDone(
                    outcome.restoredCount,
                    outcome.rolledBackCount,
                  )
                : (outcome.errorMessage ?? al.common_failed),
          ),
          duration: const Duration(seconds: 6),
        ),
      );
      await _refreshUndoPoint();
    } finally {
      if (mounted) setState(() => _isScanning = false);
    }
  }

  /// 删除被系统拦截的提示弹窗。
  ///
  /// 需要弹窗而非 snackbar：这不是一次性告知，而是需要用户去系统设置
  /// 处理（或手动删除）的可操作事项，且要明确"号码已合并"以免用户以为白做了。
  Future<void> _showDeleteBlockedDialog(int blockedCount) async {
    final al = AppLocalizations.of(context);
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.contactMerge_deleteBlockedTitle),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              al.contactMerge_deleteBlockedMsg(blockedCount),
              style: PfmTypography.bodyLarge,
            ),
            const SizedBox(height: 12),
            Text(
              al.contactMerge_deleteBlockedHint,
              style: PfmTypography.bodySmall.copyWith(
                color: Theme.of(ctx).colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              al.contactMerge_keepUndoHint,
              style: PfmTypography.bodySmall.copyWith(
                color: PfmColors.primary,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_confirm),
          ),
        ],
      ),
    );
  }

  /// 刷新撤销点信息（用于 UI 显示与按钮可用性）
  Future<void> _refreshUndoPoint() async {
    final info = await ContactUndoService.info();
    if (!mounted) return;
    setState(() => _undoInfo = info);
  }

  Future<void> _selectTargetFolder() async {
    final result = await Navigator.push<FolderPickResult>(
      context,
      MaterialPageRoute(builder: (context) => const FolderPickerPage()),
    );
    if (result != null) {
      await _backupService.setTargetFolder(result.id, result.name);
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    // 桌面端显示"仅移动端可用"提示
    if (isDesktop()) {
      return Scaffold(
        appBar: AppBar(title: Text(al.contactBackup_title), centerTitle: true),
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.phone_android,
                size: 64,
                color: Theme.of(
                  context,
                ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
              ),
              const SizedBox(height: 16),
              Text(
                al.contactBackup_mobileOnly,
                style: TextStyle(
                  fontSize: 18,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                al.contactBackup_mobileOnlyDesc,
                style: TextStyle(
                  fontSize: 14,
                  color: Theme.of(
                    context,
                  ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
                ),
              ),
            ],
          ),
        ),
      );
    }

    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: Text(al.contactBackup_title)),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    return Scaffold(
      appBar: AppBar(title: Text(al.contactBackup_title), centerTitle: true),
      body: ListView(
        children: [
          // 状态卡片
          _buildStatusCard(),

          // 备份说明
          _buildInfoCard(),

          // 目标文件夹
          ListTile(
            title: Text(al.contactBackup_backupTo),
            subtitle: Text(
              _backupService.config.targetFolderName.isEmpty
                  ? al.contactBackup_notSetRoot
                  : _backupService.config.targetFolderName,
            ),
            trailing: const Icon(Icons.chevron_right),
            onTap: _selectTargetFolder,
          ),

          const Divider(height: 1),

          // 扫描重复联系人（只读，不改数据）
          ListTile(
            leading: _isScanning
                ? const SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.content_copy),
            title: Text(al.contactMerge_scan),
            subtitle: Text(al.contactMerge_suspiciousHint),
            trailing: const Icon(Icons.chevron_right),
            onTap: _isScanning ? null : _scanDuplicates,
          ),

          // 撤销上次合并（无撤销点时禁用）
          ListTile(
            enabled: _undoInfo != null && !_isScanning,
            leading: const Icon(Icons.undo),
            title: Text(al.contactMerge_undo),
            subtitle: Text(
              _undoInfo == null
                  ? al.contactMerge_undoNone
                  : al.contactMerge_undoPoint(
                      _undoInfo!.createdAt.toString().split('.').first,
                      _undoInfo!.modifiedCount,
                      _undoInfo!.deletedCount,
                    ),
            ),
            onTap: _undoInfo == null || _isScanning ? null : _undoLastMerge,
          ),

          // 恢复通讯录（从服务器备份导入并与本地合并）
          ListTile(
            enabled: !_isRestoring && !_isScanning,
            leading: _isRestoring
                ? const SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.restore),
            title: Text(al.contactRestore_title),
            subtitle: Text(al.contactRestore_pickFile),
            trailing: const Icon(Icons.chevron_right),
            onTap: _isRestoring || _isScanning ? null : _restoreFromBackup,
          ),

          const Divider(height: 1),

          // 备份按钮
          Padding(
            padding: const EdgeInsets.all(16),
            child: ElevatedButton.icon(
              onPressed:
                  _result.status == ContactBackupStatus.loading ||
                      _result.status == ContactBackupStatus.exporting ||
                      _result.status == ContactBackupStatus.uploading
                  ? null
                  : _startBackup,
              icon: const Icon(Icons.backup),
              label: Text(al.contactBackup_backupNow),
              style: ElevatedButton.styleFrom(
                padding: const EdgeInsets.symmetric(vertical: 16),
              ),
            ),
          ),

          // 备份格式说明
          _buildFormatInfo(),
        ],
      ),
    );
  }

  Widget _buildStatusCard() {
    final al = AppLocalizations.of(context);
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case ContactBackupStatus.idle:
        statusColor = Theme.of(
          context,
        ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5);
        statusText = al.contactBackup_statusReady;
        statusIcon = Icons.contacts_outlined;
        break;
      case ContactBackupStatus.loading:
        statusColor = PfmColors.primary;
        statusText = al.contactBackup_statusLoading;
        statusIcon = Icons.hourglass_empty;
        break;
      case ContactBackupStatus.exporting:
        statusColor = PfmColors.primary;
        statusText = al.contactBackup_statusExporting(
          _result.exportedContacts,
          _result.totalContacts,
        );
        statusIcon = Icons.import_export;
        break;
      case ContactBackupStatus.uploading:
        statusColor = PfmColors.warning;
        statusText = al.contactBackup_statusUploading;
        statusIcon = Icons.cloud_upload;
        break;
      case ContactBackupStatus.completed:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusCompleted;
        statusIcon = Icons.check_circle;
        break;
      case ContactBackupStatus.skipped:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusSkipped;
        statusIcon = Icons.check_circle_outline;
        break;
      case ContactBackupStatus.error:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_statusFailed;
        statusIcon = Icons.error_outline;
        break;
      case ContactBackupStatus.noPermission:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_needPermission;
        statusIcon = Icons.lock_outline;
        break;
    }

    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Row(
              children: [
                Icon(statusIcon, color: statusColor, size: 48),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        statusText,
                        style: TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                          color: statusColor,
                        ),
                      ),
                      if (_result.errorMessage != null) ...[
                        const SizedBox(height: 4),
                        Text(
                          _result.errorMessage!,
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                      if (_result.totalContacts > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          al.contactBackup_totalContacts(_result.totalContacts),
                          style: TextStyle(
                            fontSize: 14,
                            color: Theme.of(
                              context,
                            ).colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            ),
            if (_result.status == ContactBackupStatus.exporting ||
                _result.status == ContactBackupStatus.uploading) ...[
              const SizedBox(height: 16),
              LinearProgressIndicator(
                value:
                    _result.status == ContactBackupStatus.exporting &&
                        _result.totalContacts > 0
                    ? _result.exportedContacts / _result.totalContacts
                    : null,
              ),
            ],
            if (_result.lastBackupTime != null) ...[
              const SizedBox(height: 8),
              Text(
                al.contactBackup_lastBackup(_result.lastBackupTime!.toString()),
                style: TextStyle(
                  fontSize: 12,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildInfoCard() {
    final al = AppLocalizations.of(context);
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.info_outline, color: PfmColors.primary),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_instructions,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: PfmColors.primary,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(al.contactBackup_instr1),
            Text(al.contactBackup_instr2),
            Text(al.contactBackup_instr3),
            Text(al.contactBackup_instr4),
            Text(al.contactBackup_instr5),
          ],
        ),
      ),
    );
  }

  Widget _buildFormatInfo() {
    final al = AppLocalizations.of(context);
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.description_outlined,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_howToRestore,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(al.contactBackup_restoreStep1),
            Text(al.contactBackup_restoreStep2),
            Text(al.contactBackup_restoreStep3),
            const SizedBox(height: 8),
            Text(
              al.contactRestore_supportedPlatforms,
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
