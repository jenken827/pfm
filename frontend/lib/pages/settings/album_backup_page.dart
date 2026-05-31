import 'dart:io';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/services/album_backup_service.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:photo_manager/photo_manager.dart';

/// 相册自动备份设置页面
class AlbumBackupPage extends StatefulWidget {
  const AlbumBackupPage({super.key});

  @override
  State<AlbumBackupPage> createState() => _AlbumBackupPageState();
}

class _AlbumBackupPageState extends State<AlbumBackupPage> {
  late AlbumBackupService _backupService;
  AlbumBackupConfig _config = const AlbumBackupConfig();
  AlbumBackupResult _result = const AlbumBackupResult();
  List<AssetPathEntity> _albums = [];
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _initService();
  }

  Future<void> _initService() async {
    _backupService = await AlbumBackupService.instance();
    _config = _backupService.config;
    _result = _backupService.result;

    // 监听状态变化
    _backupService.statusStream.listen((result) {
      if (mounted) {
        setState(() => _result = result);
      }
    });

    // 加载相册列表
    await _loadAlbums();

    setState(() => _isLoading = false);
  }

  Future<void> _loadAlbums() async {
    final albums = await _backupService.getAlbums();
    setState(() => _albums = albums);
  }

  Future<void> _saveConfig(AlbumBackupConfig config) async {
    await _backupService.saveConfig(config);
    setState(() => _config = config);
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
      await _saveConfig(_config.copyWith(targetParentId: result.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: const Text('相册备份')),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    // 仅在移动端显示
    if (!Platform.isAndroid && !Platform.isIOS) {
      return Scaffold(
        appBar: AppBar(title: const Text('相册备份')),
        body: const Center(
          child: Text('相册备份仅支持移动端'),
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('相册自动备份'),
        centerTitle: true,
        actions: [
          TextButton(
            onPressed: _startBackup,
            child: const Text('立即备份'),
          ),
        ],
      ),
      body: ListView(
        children: [
          // 状态卡片
          _buildStatusCard(),

          // 开关
          SwitchListTile(
            title: const Text('开启自动备份'),
            subtitle: const Text('自动备份新照片到云端'),
            value: _config.enabled,
            onChanged: (value) {
              _saveConfig(_config.copyWith(enabled: value));
            },
          ),

          // WiFi 限制
          SwitchListTile(
            title: const Text('仅 WiFi 备份'),
            subtitle: const Text('节省移动数据流量'),
            value: _config.wifiOnly,
            onChanged: _config.enabled
                ? (value) {
                    _saveConfig(_config.copyWith(wifiOnly: value));
                  }
                : null,
          ),

          // 备份视频
          SwitchListTile(
            title: const Text('备份视频'),
            subtitle: const Text('包含相册中的视频文件'),
            value: _config.backupVideos,
            onChanged: _config.enabled
                ? (value) {
                    _saveConfig(_config.copyWith(backupVideos: value));
                  }
                : null,
          ),

          const Divider(),

          // 目标文件夹
          ListTile(
            title: const Text('备份到'),
            subtitle: Text(
              _config.targetParentId.isEmpty
                  ? '未设置（将备份到根目录）'
                  : _config.targetParentId,
            ),
            trailing: const Icon(Icons.chevron_right),
            onTap: _selectTargetFolder,
          ),

          const Divider(),

          // 相册选择
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              '选择要备份的相册',
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ),

          // 相册列表
          ..._albums.map((album) => _buildAlbumTile(album)),

          const SizedBox(height: 32),
        ],
      ),
    );
  }

  Widget _buildStatusCard() {
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case AlbumBackupStatus.idle:
        statusColor = Colors.grey;
        statusText = '空闲';
        statusIcon = Icons.cloud_upload_outlined;
        break;
      case AlbumBackupStatus.scanning:
        statusColor = Colors.blue;
        statusText = '扫描中...';
        statusIcon = Icons.search;
        break;
      case AlbumBackupStatus.uploading:
        statusColor = Colors.orange;
        statusText = '备份中 ${_result.uploadedPhotos}/${_result.newPhotos}';
        statusIcon = Icons.cloud_upload;
        break;
      case AlbumBackupStatus.completed:
        statusColor = Colors.green;
        statusText = '备份完成';
        statusIcon = Icons.cloud_done;
        break;
      case AlbumBackupStatus.error:
        statusColor = Colors.red;
        statusText = '备份失败';
        statusIcon = Icons.error_outline;
        break;
      case AlbumBackupStatus.noPermission:
        statusColor = Colors.red;
        statusText = '需要相册权限';
        statusIcon = Icons.lock_outline;
        break;
      case AlbumBackupStatus.noNetwork:
        statusColor = Colors.orange;
        statusText = '无网络';
        statusIcon = Icons.wifi_off;
        break;
      case AlbumBackupStatus.waitingForWifi:
        statusColor = Colors.orange;
        statusText = '等待 WiFi';
        statusIcon = Icons.wifi;
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
                Icon(statusIcon, color: statusColor, size: 32),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        statusText,
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.bold,
                          color: statusColor,
                        ),
                      ),
                      if (_result.errorMessage != null)
                        Text(
                          _result.errorMessage!,
                          style: TextStyle(
                            fontSize: 12,
                            color: Colors.grey[600],
                          ),
                        ),
                    ],
                  ),
                ),
              ],
            ),
            if (_result.status == AlbumBackupStatus.uploading) ...[
              const SizedBox(height: 12),
              LinearProgressIndicator(
                value: _result.newPhotos > 0
                    ? _result.uploadedPhotos / _result.newPhotos
                    : 0,
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

  Widget _buildAlbumTile(AssetPathEntity album) {
    final isSelected = _config.backupAlbumIds.isEmpty ||
        _config.backupAlbumIds.contains(album.id);

    return FutureBuilder<int>(
      future: album.assetCountAsync,
      builder: (context, snapshot) {
        final count = snapshot.data ?? 0;
        return CheckboxListTile(
          title: Text(album.name),
          subtitle: Text('$count 张照片'),
          value: isSelected,
          onChanged: _config.enabled
              ? (value) {
                  List<String> newIds;
                  if (_config.backupAlbumIds.isEmpty) {
                    // 从"全部"模式切换到"选择"模式
                    newIds = _albums
                        .where((a) => a.id != album.id)
                        .map((a) => a.id)
                        .toList();
                  } else {
                    newIds = List.from(_config.backupAlbumIds);
                    if (value == true) {
                      newIds.add(album.id);
                    } else {
                      newIds.remove(album.id);
                    }
                    // 如果全部选中，切换到"全部"模式
                    if (newIds.length == _albums.length) {
                      newIds = [];
                    }
                  }
                  _saveConfig(_config.copyWith(backupAlbumIds: newIds));
                }
              : null,
        );
      },
    );
  }
}
