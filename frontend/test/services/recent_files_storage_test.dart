import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:hive_ce/hive.dart';
import 'package:pfm/services/recent_files_storage.dart';

/// 「最近文件」点击无响应的回归测试。
///
/// 根因：`RecentFilesStorage` 把整条记录（含嵌套的 `fileJson`）写进 Hive，
/// Hive 解码后**嵌套** map 的运行时类型是 `Map<dynamic, dynamic>`，
/// 首页点击时写的是 `file['fileJson'] as Map<String, dynamic>?` ——
/// 这行在 tap 回调里抛 TypeError（被 PlatformDispatcher.onError 吞掉），
/// 于是「点了没反应」，连后面的 `getFileInfo` 兜底都走不到。
void main() {
  group('asStringMap', () {
    test('Map<dynamic, dynamic>（Hive 解码后的真实类型）可归一化', () {
      final fromHive = <dynamic, dynamic>{
        'id': 'file-1',
        'name': 'a.jpg',
        'type': 2,
        'size': 1024,
      };

      // 先复现旧写法必然抛的类型错误
      expect(
        () => fromHive as Map<String, dynamic>,
        throwsA(isA<TypeError>()),
        reason: '旧写法 `file["fileJson"] as Map<String, dynamic>` 就是在这里炸的',
      );

      final parsed = asStringMap(fromHive);
      expect(parsed, isA<Map<String, dynamic>>());
      expect(parsed!['id'], 'file-1');
      expect(parsed['name'], 'a.jpg');
    });

    test('本来就是 Map<String, dynamic> 时原样可用', () {
      final parsed = asStringMap(<String, dynamic>{'id': 'x'});
      expect(parsed, isNotNull);
      expect(parsed!['id'], 'x');
    });

    test('null / 非 map 值返回 null（走 API 兜底，不再抛异常）', () {
      expect(asStringMap(null), isNull);
      expect(asStringMap('not a map'), isNull);
      expect(asStringMap(42), isNull);
    });
  });

  group('Hive 往返', () {
    test('写入后重新打开 box，嵌套 map 是 Map<dynamic, dynamic>', () async {
      final dir = await Directory.systemTemp.createTemp('pfm_recent_files');
      Hive.init(dir.path);

      var box = await Hive.openBox<Map>('recent_files_roundtrip');
      await box.put('user:file-1', {
        'fileId': 'file-1',
        'fileName': 'a.jpg',
        'fileJson': <String, dynamic>{'id': 'file-1', 'name': 'a.jpg'},
      });
      // 关键：必须关掉再打开，否则读到的是 Hive 内存缓存里的原始对象
      // （类型仍是写入时的 Map<String, dynamic>），验不出解码行为
      await box.close();
      box = await Hive.openBox<Map>('recent_files_roundtrip');

      final raw = box.get('user:file-1')!;
      // 读取路径与 RecentFilesStorage.getRecentFiles 一致
      final record = Map<String, dynamic>.from(raw);
      final nested = record['fileJson'];

      expect(
        nested,
        isA<Map<dynamic, dynamic>>(),
        reason: 'Hive 解码后嵌套 map 丢失了 Map<String, dynamic> 类型',
      );
      expect(nested is Map<String, dynamic>, isFalse);
      expect(asStringMap(nested)?['name'], 'a.jpg');

      await box.close();
      await dir.delete(recursive: true);
    });
  });
}
