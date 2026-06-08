import 'package:flutter/material.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/contact_backup_service.dart';
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

    setState(() => _isLoading = false);
  }

  Future<void> _startBackup() async {
    await _backupService.startBackup();
  }

  Future<void> _selectTargetFolder() async {
    final result = await Navigator.push<FolderPickResult>(
      context,
      MaterialPageRoute(
        builder: (context) => const FolderPickerPage(),
      ),
    );
    if (result != null) {
      await _backupService.setTargetFolder(result.id, result.name);
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    // 桌面端显示"仅移动端可用"提示
    if (isDesktop()) {
      return Scaffold(
        appBar: AppBar(
          title: Text(al.contactBackup_title),
          centerTitle: true,
        ),
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(Icons.phone_android, size: 64, color: PfmColors.lightTextMuted),
              const SizedBox(height: 16),
              Text(
                al.contactBackup_mobileOnly,
                style: TextStyle(fontSize: 18, color: PfmColors.lightTextSecondary),
              ),
              const SizedBox(height: 8),
              Text(
                al.contactBackup_mobileOnlyDesc,
                style: TextStyle(fontSize: 14, color: PfmColors.lightTextMuted),
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
      appBar: AppBar(
        title: Text(al.contactBackup_title),
        centerTitle: true,
      ),
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

          // 备份按钮
          Padding(
            padding: const EdgeInsets.all(16),
            child: ElevatedButton.icon(
              onPressed: _result.status == ContactBackupStatus.loading ||
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
    final al = AppLocalizations.of(context)!;
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case ContactBackupStatus.idle:
        statusColor = PfmColors.lightTextMuted;
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
        statusText = al.contactBackup_statusExporting(_result.exportedContacts, _result.totalContacts);
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
                            color: PfmColors.lightTextSecondary,
                          ),
                        ),
                      ],
                      if (_result.totalContacts > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          al.contactBackup_totalContacts(_result.totalContacts),
                          style: TextStyle(
                            fontSize: 14,
                            color: PfmColors.lightTextSecondary,
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
                value: _result.status == ContactBackupStatus.exporting &&
                        _result.totalContacts > 0
                    ? _result.exportedContacts / _result.totalContacts
                    : null,
              ),
            ],
            if (_result.lastBackupTime != null) ...[
              const SizedBox(height: 8),
              Text(
                al.contactBackup_lastBackup(_result.lastBackupTime!.toString()),
                style: TextStyle(fontSize: 12, color: PfmColors.lightTextSecondary),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildInfoCard() {
    final al = AppLocalizations.of(context)!;
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
    final al = AppLocalizations.of(context)!;
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.description_outlined, color: PfmColors.lightTextSecondary),
                const SizedBox(width: 8),
                Text(
                  al.contactBackup_howToRestore,
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: PfmColors.lightTextSecondary,
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
              style: TextStyle(fontSize: 12, color: PfmColors.lightTextSecondary),
            ),
          ],
        ),
      ),
    );
  }
}
