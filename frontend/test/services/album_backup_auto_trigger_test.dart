import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/album_backup_service.dart';

/// 相册自动备份「自动触发」条件判定的纯逻辑单测。
///
/// 回归背景：`autoTrigger()` 曾把空的 `backupAlbumIds` 当作「未配置」直接 return，
/// 而设置页把「全选相册」归一化为空列表、`_getPhotosToBackup` 也把空列表当
/// 「备份全部相册」。结果就是——用户开了自动备份也选了「全部相册」，冷启动 /
/// 回前台却什么都不会发生，只能手点「立即备份」。
void main() {
  group('autoBackupSkipReason', () {
    test('默认配置（全部相册 = 空列表）可以自动触发', () {
      const config = AlbumBackupConfig(
        enabled: true,
        targetParentId: 'folder-1',
      );

      expect(
        autoBackupSkipReason(config, isBackingUp: false),
        isNull,
        reason: '空 backupAlbumIds 表示「全部相册」，不得判定为未配置',
      );
    });

    test('显式选中若干相册同样可以自动触发', () {
      const config = AlbumBackupConfig(
        enabled: true,
        targetParentId: 'folder-1',
        backupAlbumIds: ['album-a', 'album-b'],
      );

      expect(autoBackupSkipReason(config, isBackingUp: false), isNull);
    });

    test('开关关闭时跳过，并给出原因', () {
      const config = AlbumBackupConfig(targetParentId: 'folder-1');

      expect(autoBackupSkipReason(config, isBackingUp: false), '自动备份未启用');
    });

    test('未选择目标文件夹时跳过，并给出原因', () {
      const config = AlbumBackupConfig(enabled: true);

      expect(autoBackupSkipReason(config, isBackingUp: false), '未选择目标文件夹');
    });

    test('正在备份时不重复触发', () {
      const config = AlbumBackupConfig(
        enabled: true,
        targetParentId: 'folder-1',
      );

      expect(autoBackupSkipReason(config, isBackingUp: true), '备份正在进行中');
    });
  });
}
