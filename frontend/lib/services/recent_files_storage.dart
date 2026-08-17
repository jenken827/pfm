import 'package:hive_ce/hive.dart';
import 'package:pfm/services/local_storage.dart';
import 'package:get_it/get_it.dart';

/// 本地最近浏览记录存储（per-user，基于 Hive）
///
/// 每个用户独立的浏览历史，退出登录时自动隔离。
/// 最多保留 [_maxRecords] 条记录，超出时删除最早的。
class RecentFilesStorage {
  static const _boxName = 'recent_files';
  static const _maxRecords = 100;

  static RecentFilesStorage? _instance;
  late Box<Map> _box;

  RecentFilesStorage._();

  static Future<RecentFilesStorage> instance() async {
    if (_instance != null) return _instance!;
    _instance = RecentFilesStorage._();
    _instance!._box = await Hive.openBox<Map>(_boxName);
    return _instance!;
  }

  /// 获取当前用户 ID
  String? get _currentUserId {
    final ls = GetIt.I.get<LocalStorage>();
    return ls.user?.name;
  }

  /// 记录文件访问
  Future<void> recordAccess({
    required String fileId,
    required String fileName,
    required int fileType,
    required int fileSize,
    String? parentId,
    Map<String, dynamic>? fileJson,
  }) async {
    final userId = _currentUserId;
    if (userId == null) return;

    final key = '$userId:$fileId';
    final record = {
      'fileId': fileId,
      'fileName': fileName,
      'fileType': fileType,
      'fileSize': fileSize,
      'parentId': parentId ?? '',
      'fileJson': fileJson,
      'accessedAt': DateTime.now().millisecondsSinceEpoch,
    };
    await _box.put(key, record);

    // 超出上限时清理当前用户的旧记录
    await _trimRecords(userId);
  }

  /// 获取当前用户的最近浏览文件
  List<Map<String, dynamic>> getRecentFiles({int limit = 20}) {
    final userId = _currentUserId;
    if (userId == null) return [];

    final prefix = '$userId:';
    final entries = <Map<String, dynamic>>[];

    for (final key in _box.keys) {
      if (key is String && key.startsWith(prefix)) {
        final value = _box.get(key);
        if (value != null) {
          entries.add(Map<String, dynamic>.from(value));
        }
      }
    }

    // 按访问时间降序排序
    entries.sort(
      (a, b) => (b['accessedAt'] as int).compareTo(a['accessedAt'] as int),
    );

    return entries.take(limit).toList();
  }

  /// 清除当前用户的浏览记录
  Future<void> clearCurrentUser() async {
    final userId = _currentUserId;
    if (userId == null) return;

    final prefix = '$userId:';
    final keysToDelete = _box.keys
        .where((k) => k is String && k.startsWith(prefix))
        .toList();
    await _box.deleteAll(keysToDelete);
  }

  /// 清理超出上限的旧记录
  Future<void> _trimRecords(String userId) async {
    final prefix = '$userId:';
    final userKeys = _box.keys
        .where((k) => k is String && k.startsWith(prefix))
        .toList();

    if (userKeys.length <= _maxRecords) return;

    // 按访问时间排序，删除最早的
    final entries = userKeys.map((k) {
      final v = _box.get(k);
      return MapEntry(
        k as String,
        v != null ? (v['accessedAt'] as int? ?? 0) : 0,
      );
    }).toList();

    entries.sort((a, b) => a.value.compareTo(b.value));

    final toDelete = entries
        .take(entries.length - _maxRecords)
        .map((e) => e.key);
    await _box.deleteAll(toDelete);
  }
}
