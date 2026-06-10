import 'dart:io';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/design/design_tokens.dart';
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

  Future<void> _startRebackup() async {
    final al = AppLocalizations.of(context)!;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(al.albumBackup_rebackup),
        content: Text(al.albumBackup_rebackupConfirm),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(al.common_cancel),
          ),
          ElevatedButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(al.albumBackup_rebackup),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      await _backupService.startBackup(force: true);
    }
  }

  Future<void> _selectTargetFolder() async {
    final result = await Navigator.push<FolderPickResult>(
      context,
      MaterialPageRoute(
        builder: (context) => const FolderPickerPage(),
      ),
    );
    if (result != null) {
      await _saveConfig(_config.copyWith(
        targetParentId: result.id,
        targetFolderName: result.name,
      ));
    }
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    if (_isLoading) {
      return Scaffold(
        appBar: AppBar(title: Text(al.albumBackup_title)),
        body: const Center(child: CircularProgressIndicator()),
      );
    }

    // 仅在移动端显示
    if (!Platform.isAndroid && !Platform.isIOS) {
      return Scaffold(
        appBar: AppBar(title: Text(al.albumBackup_title)),
        body: Center(
          child: Text(al.albumBackup_mobileOnly),
        ),
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: Text(al.albumBackup_autoTitle),
        centerTitle: true,
        actions: [
          TextButton(
            onPressed: _startBackup,
            child: Text(al.albumBackup_backupNow),
          ),
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert),
            onSelected: (value) {
              if (value == 'rebackup') {
                _startRebackup();
              }
            },
            itemBuilder: (context) => [
              PopupMenuItem(
                value: 'rebackup',
                child: ListTile(
                  leading: const Icon(Icons.refresh),
                  title: Text(al.albumBackup_rebackup),
                  subtitle: Text(al.albumBackup_rebackupHint),
                ),
              ),
            ],
          ),
        ],
      ),
      body: ListView(
        children: [
          // 状态卡片
          _buildStatusCard(),

          // 开关
          SwitchListTile(
            title: Text(al.albumBackup_enableAuto),
            subtitle: Text(al.albumBackup_autoHint),
            value: _config.enabled,
            onChanged: (value) {
              _saveConfig(_config.copyWith(enabled: value));
            },
          ),

          // WiFi 限制
          SwitchListTile(
            title: Text(al.albumBackup_wifiOnly),
            subtitle: Text(al.albumBackup_wifiOnlyHint),
            value: _config.wifiOnly,
            onChanged: _config.enabled
                ? (value) {
                    _saveConfig(_config.copyWith(wifiOnly: value));
                  }
                : null,
          ),

          // 备份视频
          SwitchListTile(
            title: Text(al.albumBackup_backupVideos),
            subtitle: Text(al.albumBackup_backupVideosHint),
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
            title: Text(al.albumBackup_backupTo),
            subtitle: Text(
              _config.targetParentId.isEmpty
                  ? al.albumBackup_notSetRoot
                  : _config.targetFolderName.isEmpty
                      ? _config.targetParentId
                      : _config.targetFolderName,
            ),
            trailing: const Icon(Icons.chevron_right),
            onTap: _selectTargetFolder,
          ),

          const Divider(),

          // 相册选择
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              al.albumBackup_selectAlbums,
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
    final al = AppLocalizations.of(context)!;
    Color statusColor;
    String statusText;
    IconData statusIcon;

    switch (_result.status) {
      case AlbumBackupStatus.idle:
        statusColor = Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5);
        statusText = al.albumBackup_statusIdle;
        statusIcon = Icons.cloud_upload_outlined;
        break;
      case AlbumBackupStatus.scanning:
        statusColor = PfmColors.primary;
        statusText = al.albumBackup_statusScanning;
        statusIcon = Icons.search;
        break;
      case AlbumBackupStatus.uploading:
        statusColor = PfmColors.warning;
        statusText = al.albumBackup_statusBackingUp(_result.uploadedPhotos, _result.newPhotos);
        statusIcon = Icons.cloud_upload;
        break;
      case AlbumBackupStatus.completed:
        statusColor = PfmColors.success;
        statusText = al.albumBackup_statusCompleted;
        statusIcon = Icons.cloud_done;
        break;
      case AlbumBackupStatus.error:
        statusColor = PfmColors.error;
        statusText = al.albumBackup_statusFailed;
        statusIcon = Icons.error_outline;
        break;
      case AlbumBackupStatus.noPermission:
        statusColor = PfmColors.error;
        statusText = al.albumBackup_needPermission;
        statusIcon = Icons.lock_outline;
        break;
      case AlbumBackupStatus.noNetwork:
        statusColor = PfmColors.warning;
        statusText = al.albumBackup_noNetwork;
        statusIcon = Icons.wifi_off;
        break;
      case AlbumBackupStatus.waitingForWifi:
        statusColor = PfmColors.warning;
        statusText = al.albumBackup_waitingWifi;
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
                            color: Theme.of(context).colorScheme.onSurfaceVariant,
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
                al.albumBackup_lastBackup(_result.lastBackupTime!.toString()),
                style: TextStyle(fontSize: 12, color: Theme.of(context).colorScheme.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildAlbumTile(AssetPathEntity album) {
    final al = AppLocalizations.of(context)!;
    final isSelected = _config.backupAlbumIds.isEmpty ||
        _config.backupAlbumIds.contains(album.id);

    return FutureBuilder<int>(
      future: album.assetCountAsync,
      builder: (context, snapshot) {
        final count = snapshot.data ?? 0;
        return CheckboxListTile(
          title: Text(album.name),
          subtitle: Text(al.albumBackup_photoCount(count)),
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
