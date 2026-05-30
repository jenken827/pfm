import 'package:flutter/material.dart';
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
  String _targetParentId = '';
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
    await _backupService.startBackup(targetParentId: _targetParentId);
  }

  Future<void> _selectTargetFolder() async {
    final result = await Navigator.push<String>(
      context,
      MaterialPageRoute(
        builder: (context) => const FolderPickerPage(),
      ),
    );
    if (result != null) {
      setState(() => _targetParentId = result);
    }
  }

  @override
  Widget build(BuildContext context) {
    // 桌面端显示"仅移动端可用"提示
    if (isDesktop()) {
      return Scaffold(
        appBar: AppBar(
          title: const Text('通讯录备份'),
          centerTitle: true,
        ),
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(Icons.phone_android, size: 64, color: Colors.grey[400]),
              const SizedBox(height: 16),
              Text(
                '通讯录备份仅在移动端可用',
                style: TextStyle(fontSize: 18, color: Colors.grey[600]),
              ),
              const SizedBox(height: 8),
              Text(
                '请在 Android 或 iOS 设备上使用此功能',
                style: TextStyle(fontSize: 14, color: Colors.grey[500]),
              ),
            ],
          ),
        ),
      );
    }

    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: const Text('通讯录备份')),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('通讯录备份'),
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
            title: const Text('备份到'),
            subtitle: Text(
              _targetParentId.isEmpty
                  ? '未设置（将备份到根目录）'
                  : _targetParentId,
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
              label: const Text('立即备份通讯录'),
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
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case ContactBackupStatus.idle:
        statusColor = Colors.grey;
        statusText = '就绪';
        statusIcon = Icons.contacts_outlined;
        break;
      case ContactBackupStatus.loading:
        statusColor = Colors.blue;
        statusText = '加载联系人中...';
        statusIcon = Icons.hourglass_empty;
        break;
      case ContactBackupStatus.exporting:
        statusColor = Colors.blue;
        statusText = '导出中 ${_result.exportedContacts}/${_result.totalContacts}';
        statusIcon = Icons.import_export;
        break;
      case ContactBackupStatus.uploading:
        statusColor = Colors.orange;
        statusText = '上传中...';
        statusIcon = Icons.cloud_upload;
        break;
      case ContactBackupStatus.completed:
        statusColor = Colors.green;
        statusText = '备份完成';
        statusIcon = Icons.check_circle;
        break;
      case ContactBackupStatus.error:
        statusColor = Colors.red;
        statusText = '备份失败';
        statusIcon = Icons.error_outline;
        break;
      case ContactBackupStatus.noPermission:
        statusColor = Colors.red;
        statusText = '需要通讯录权限';
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
                            color: Colors.grey[600],
                          ),
                        ),
                      ],
                      if (_result.totalContacts > 0) ...[
                        const SizedBox(height: 4),
                        Text(
                          '共 ${_result.totalContacts} 个联系人',
                          style: TextStyle(
                            fontSize: 14,
                            color: Colors.grey[600],
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
                '上次备份: ${_result.lastBackupTime}',
                style: TextStyle(fontSize: 12, color: Colors.grey[600]),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildInfoCard() {
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.info_outline, color: Colors.blue[700]),
                const SizedBox(width: 8),
                Text(
                  '备份说明',
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: Colors.blue[700],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            const Text('• 备份将导出所有联系人为 vCard (.vcf) 格式'),
            const Text('• vCard 是通用的联系人格式，可导入到任何设备'),
            const Text('• 备份文件将上传到您选择的服务器文件夹'),
            const Text('• 包含：姓名、电话、邮箱、公司、备注等信息'),
          ],
        ),
      ),
    );
  }

  Widget _buildFormatInfo() {
    return Card(
      margin: const EdgeInsets.all(16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.description_outlined, color: Colors.grey[700]),
                const SizedBox(width: 8),
                Text(
                  '如何恢复联系人',
                  style: TextStyle(
                    fontWeight: FontWeight.bold,
                    color: Colors.grey[700],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            const Text('1. 从服务器下载 .vcf 备份文件'),
            const Text('2. 在手机上打开该文件'),
            const Text('3. 系统会自动提示导入联系人'),
            const SizedBox(height: 8),
            Text(
              '支持 Android、iOS、Outlook、Google Contacts 等',
              style: TextStyle(fontSize: 12, color: Colors.grey[600]),
            ),
          ],
        ),
      ),
    );
  }
}
