import 'dart:io';

import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pfm/services/logger.dart';

/// 下载目录解析与「发布到公共下载目录」。
///
/// 两段式设计（Android）：
///   1. 下载中 → 应用私有目录（`getDownloadsDirectory()`，即
///      `/storage/emulated/0/Android/data/<pkg>/files/Download`）。
///      分区存储下私有目录无需任何敏感权限，多线程 Range 分片与断点续传
///      （RandomAccessFile.setPosition）可以正常工作。
///   2. 下载完成后 → 发布到公共「下载」目录（`Environment.DIRECTORY_DOWNLOADS`）。
///      经由原生通道走 MediaStore，由系统组件代写；**不需要 MANAGE_EXTERNAL_STORAGE**。
///
/// 为什么不直接下载到公共目录：
/// 分区存储对公共目录的写入是路径级白名单（不是 unix 权限位）。没有
/// MANAGE_EXTERNAL_STORAGE 时，rename / copy+unlink 一律在 FUSE 层被拒（EACCES）；
/// 唯一合法途径是 MediaStore，而它只能 ContentResolver 流式写，无法满足
/// 分片随机写 + 续传。故采用「私有目录下载 → 完成后发布」。
///
/// 代价：发布瞬间需要额外一份磁盘空间（拷完即删源），且下载中的文件对其他应用不可见。
///
/// 桌面 / iOS 无分区存储限制，目录本身就是用户可见的下载目录，不触发发布。
class DownloadDir {
  DownloadDir._();

  static const MethodChannel _channel = MethodChannel('pfm/public_storage');

  /// 已验证可用的公共下载目录（进程内缓存，免重复探测）
  static String? _cachedPublishDir;

  /// 已判定「不可发布」的标记（评测失败后不再重复尝试，避免每次下载都白跑一遍）
  static bool _publishUnavailable = false;

  // ================================================================
  // 下载写入目录
  // ================================================================

  /// 下载文件的写入目录（Android 为应用私有目录）。
  ///
  /// 返回值以路径分隔符结尾，调用方直接拼接文件名即可。
  static Future<String> resolveWriteDir() async {
    final dir = await getDownloadsDirectory();
    if (dir == null) {
      // 外部存储不可用（极少见）：调用方 catch 后提示下载失败
      throw StateError('downloads directory unavailable');
    }
    return _withTrailingSeparator(dir.path);
  }

  // ================================================================
  // 公共下载目录（发布目标）
  // ================================================================

  /// 本次运行是否需要「下载后发布」到公共下载目录。
  ///
  /// 仅 Android 需要（分区存储）；其他平台下载目录本身即用户可见。
  static bool get publishNeeded => Platform.isAndroid;

  /// 解析公共下载目录（发布目标）。
  ///
  /// 返回 null 表示不可发布：非 Android、原生通道缺失，或写探测失败。
  /// 调用方应在此情况下保留私有目录中的文件，仅记录日志，不得让任务失败。
  static Future<String?> resolvePublishDir() async {
    if (!publishNeeded) return null;
    if (_publishUnavailable) return null;
    if (_cachedPublishDir != null) return _cachedPublishDir;

    final dir = await _invokeNative<String>('getPublicDownloadsDir');
    if (dir == null || dir.isEmpty) {
      getLogger().warning('public downloads dir unavailable (native channel missing)');
      _publishUnavailable = true;
      return null;
    }

    // 写探测：分区存储下对无权限路径 exists() 直接返回 false，
    // 仅判断存在性会误判，因此实际写入一个探测文件再删除。
    if (!await _probeWritable(dir)) {
      getLogger().warning('public downloads dir not writable: $dir');
      _publishUnavailable = true;
      return null;
    }

    _cachedPublishDir = dir;
    return dir;
  }

  /// 发布已完成下载的私有文件到公共下载目录。
  ///
  /// [localPath] 私有目录中的文件绝对路径；[fileName] 期望在公共目录中呈现的名称。
  ///
  /// 成功返回 [PublishResult]（含实际落地路径与可打开的 uri），
  /// 失败返回 null（调用方保留私有文件，不要视为下载失败）。
  ///
  /// 注意：本方法完成后 **不会** 删除 [localPath]，删源由调用方在确认发布成功后执行。
  static Future<PublishResult?> publish({
    required String localPath,
    required String fileName,
  }) async {
    if (!publishNeeded) return null;

    try {
      final res = await _channel.invokeMapMethod<String, dynamic>(
        'publishToDownloads',
        {'srcPath': localPath, 'fileName': fileName},
      );
      final result = PublishResult.fromMap(res);
      if (result == null) return null;
      getLogger().info(
        'published to public downloads: $localPath -> ${result.path}'
        '${result.uri != null ? ' (uri=${result.uri})' : ''}',
      );
      return result;
    } on MissingPluginException {
      return null;
    } on PlatformException catch (e) {
      getLogger().warning('publish to public downloads failed: $e');
      return null;
    } catch (e) {
      getLogger().warning('publish to public downloads failed: $e');
      return null;
    }
  }

  /// 用 content:// uri 打开已发布的公共文件。
  ///
  /// 必须走原生通道而非 `OpenFile.open(path)`：open_file 插件对文件**路径**会先做
  /// `!file.canRead()` 判断，公共目录下会进而要求 `MANAGE_EXTERNAL_STORAGE`
  /// （open_file_android `OpenFilePlugin.java:91-110`），而本应用刻意不申请该权限，
  /// 会导致「打开文件」对已发布文件必然失败。改用 MediaStore 的 uri 后，
  /// 读权限随 uri 授予，无需任何敏感权限。
  ///
  /// 返回 true 表示已成功拉起某个应用。
  static Future<bool> openPublished({
    required String uri,
    String? mimeType,
  }) async {
    if (!publishNeeded) return false;
    try {
      return await _channel.invokeMethod<bool>('openPublished', {
            'uri': uri,
            'mimeType': mimeType,
          }) ??
          false;
    } catch (e) {
      getLogger().warning('open published file failed for $uri: $e');
      return false;
    }
  }

  /// 删除已发布的公共文件（任务记录清理时调用；失败可容忍）
  static Future<bool> deletePublished(String target) async {
    if (!publishNeeded) return false;
    try {
      return await _channel.invokeMethod<bool>('deletePublicFile', {
            'target': target,
          }) ??
          false;
    } catch (e) {
      getLogger().warning('delete published file failed for $target: $e');
      return false;
    }
  }

  // ================================================================
  // 内部工具
  // ================================================================

  static String _withTrailingSeparator(String path) =>
      path.endsWith(Platform.pathSeparator) ? path : '$path${Platform.pathSeparator}';

  /// 写探测：确认目录真实可写
  static Future<bool> _probeWritable(String dirPath) async {
    try {
      final dir = Directory(dirPath);
      if (!await dir.exists()) {
        await dir.create(recursive: true);
      }
      final probe = File('$dirPath${Platform.pathSeparator}.pfm_write_probe');
      await probe.writeAsString('', flush: true);
      await probe.delete();
      return true;
    } catch (e) {
      getLogger().warning('write probe failed for $dirPath: $e');
      return false;
    }
  }

  /// 调用原生通道；通道缺失/异常时返回 null（桌面或测试环境）
  static Future<T?> _invokeNative<T>(String method) async {
    try {
      return await _channel.invokeMethod<T>(method);
    } on MissingPluginException {
      return null;
    } on PlatformException catch (e) {
      getLogger().warning('public storage channel $method failed: $e');
      return null;
    }
  }
}

/// 发布到公共下载目录的结果
///
/// - [path]：实际落地的绝对路径（重名时系统会追加 ` (1)` 后缀），用于展示/去重
/// - [uri]：MediaStore 的 `content://` uri（仅 Android API 29+ 有值），用于打开文件
class PublishResult {
  const PublishResult({required this.path, this.uri});

  final String path;
  final String? uri;

  static PublishResult? fromMap(Map<String, dynamic>? map) {
    if (map == null) return null;
    // 平台上返回的 Map 经编解码后值类型为 dynamic，用 toString 兜底避免
    // 个别设备/编解码差异导致的类型转换异常
    final path = map['path']?.toString();
    if (path == null || path.isEmpty) return null;
    final uri = map['uri']?.toString();
    return PublishResult(
      path: path,
      uri: (uri == null || uri.isEmpty) ? null : uri,
    );
  }
}
