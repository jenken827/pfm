import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/device_backup_service.dart';
import 'package:pfm/utils/platform.dart';

/// 短信与通话记录备份页面（任务9）
///
/// 移动端（Android）可用：读取系统短信/通话记录 → 导出文件 → 走后台上传。
/// 桌面端仅提示。
class DeviceBackupPage extends StatefulWidget {
  const DeviceBackupPage({super.key});

  @override
  State<DeviceBackupPage> createState() => _DeviceBackupPageState();
}

class _DeviceBackupPageState extends State<DeviceBackupPage> {
  late DeviceBackupService _service;
  bool _isLoading = true;

  DeviceBackupType _tab = DeviceBackupType.sms;

  /// 各 Tab 结果快照（用 setState 刷新）
  DeviceBackupResult _smsResult = const DeviceBackupResult();
  DeviceBackupResult _callResult = const DeviceBackupResult();

  @override
  void initState() {
    super.initState();
    _initService();
  }

  Future<void> _initService() async {
    _service = await DeviceBackupService.instance();
    _smsResult = _service.result(DeviceBackupType.sms);
    _callResult = _service.result(DeviceBackupType.callLog);

    _service
        .statusStream(DeviceBackupType.sms)
        .listen((r) => setState(() => _smsResult = r));
    _service
        .statusStream(DeviceBackupType.callLog)
        .listen((r) => setState(() => _callResult = r));

    setState(() => _isLoading = false);
  }

  DeviceBackupResult get _result =>
      _tab == DeviceBackupType.sms ? _smsResult : _callResult;

  DeviceBackupConfig get _config =>
      _service.config(_tab);

  Future<void> _startBackup() => _service.startBackup(_tab);

  Future<void> _selectTargetFolder() async {
    final r = await Navigator.push<FolderPickResult>(
      context,
      MaterialPageRoute(builder: (context) => const FolderPickerPage()),
    );
    if (r != null) {
      await _service.setTargetFolder(_tab, r.id, r.name);
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    // 桌面端显示"仅移动端可用"提示
    if (isDesktop()) {
      return Scaffold(
        appBar: AppBar(title: Text(al.deviceBackup_title), centerTitle: true),
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
        appBar: AppBar(title: Text(al.deviceBackup_title)),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    return Scaffold(
      appBar: AppBar(title: Text(al.deviceBackup_title), centerTitle: true),
      body: Column(
        children: [
          // Tab 切换：短信 / 通话记录
          TabBar(
            onTap: (i) => setState(
              () => _tab = i == 0
                  ? DeviceBackupType.sms
                  : DeviceBackupType.callLog,
            ),
            tabs: [
              Tab(text: al.deviceBackup_tabSms),
              Tab(text: al.deviceBackup_tabCallLog),
            ],
          ),
          Expanded(
            child: ListView(
              children: [
                _buildStatusCard(),
                _buildInfoCard(),
                ListTile(
                  title: Text(al.contactBackup_backupTo),
                  subtitle: Text(
                    _config.targetFolderName.isEmpty
                        ? al.contactBackup_notSetRoot
                        : _config.targetFolderName,
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _selectTargetFolder,
                ),
                const Divider(height: 1),
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: ElevatedButton.icon(
                    onPressed:
                        _result.status == DeviceBackupStatus.loading ||
                            _result.status == DeviceBackupStatus.exporting ||
                            _result.status == DeviceBackupStatus.uploading
                        ? null
                        : _startBackup,
                    icon: const Icon(Icons.backup),
                    label: Text(
                      _tab == DeviceBackupType.sms
                          ? al.deviceBackup_backupSms
                          : al.deviceBackup_backupCallLog,
                    ),
                    style: ElevatedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                  ),
                ),
                _buildFormatInfo(),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildStatusCard() {
    final al = AppLocalizations.of(context);
    final isSms = _tab == DeviceBackupType.sms;
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case DeviceBackupStatus.idle:
        statusColor = Theme.of(
          context,
        ).colorScheme.onSurfaceVariant.withValues(alpha: 0.5);
        statusText = al.contactBackup_statusReady;
        statusIcon = isSms ? Icons.sms_outlined : Icons.call_outlined;
        break;
      case DeviceBackupStatus.loading:
        statusColor = PfmColors.primary;
        statusText = al.contactBackup_statusLoading;
        statusIcon = Icons.hourglass_empty;
        break;
      case DeviceBackupStatus.exporting:
        statusColor = PfmColors.primary;
        statusText = al.deviceBackup_statusExporting(
          _result.exportedCount,
          _result.totalCount,
        );
        statusIcon = Icons.import_export;
        break;
      case DeviceBackupStatus.uploading:
        statusColor = PfmColors.warning;
        statusText = al.contactBackup_statusUploading;
        statusIcon = Icons.cloud_upload;
        break;
      case DeviceBackupStatus.completed:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusCompleted;
        statusIcon = Icons.check_circle;
        break;
      case DeviceBackupStatus.skipped:
        statusColor = PfmColors.success;
        statusText = al.contactBackup_statusSkipped;
        statusIcon = Icons.check_circle_outline;
        break;
      case DeviceBackupStatus.error:
        statusColor = PfmColors.error;
        statusText = al.contactBackup_statusFailed;
        statusIcon = Icons.error_outline;
        break;
      case DeviceBackupStatus.noPermission:
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
                      if (_result.totalCount > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          al.deviceBackup_totalCount(_result.totalCount),
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
            if (_result.status == DeviceBackupStatus.exporting ||
                _result.status == DeviceBackupStatus.uploading) ...[
              const SizedBox(height: 16),
              const LinearProgressIndicator(),
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
            Text(al.deviceBackup_instr1),
            Text(al.deviceBackup_instr2),
            Text(al.deviceBackup_instr3),
            Text(al.deviceBackup_instr4),
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
            Text(al.deviceBackup_restoreStep1),
            Text(al.deviceBackup_restoreStep2),
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
