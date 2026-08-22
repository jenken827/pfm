import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:path_provider/path_provider.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'package:share_plus/share_plus.dart';

Talker getLogger() => GetIt.I.get<Talker>();

/// 日志文件目录
Directory? _logDir;

/// 文件日志写入器
class _FileLogWriter {
  IOSink? _sink;
  File? _currentFile;
  int _currentSize = 0;
  static const int _maxFileSize = 10 * 1024 * 1024; // 10MB
  static const int _maxFiles = 3;

  Future<void> init(Directory logDir) async {
    _logDir = logDir;
    await _openFile();
  }

  Future<void> _openFile() async {
    final timestamp = DateTime.now().toString().substring(0, 10);
    _currentFile = File('${_logDir!.path}/pfm_$timestamp.log');
    _sink = _currentFile!.openWrite(mode: FileMode.append);
    _currentSize = _currentFile!.existsSync() ? await _currentFile!.length() : 0;
  }

  void write(String message) {
    if (_sink == null) return;

    try {
      final timestamp = DateTime.now().toString().substring(11, 23);
      _sink!.writeln('$timestamp $message');
      _currentSize += message.length + 1;
      // 实时落盘：进程被强杀/卡死时尽量不丢最后几条日志
      unawaited(_sink!.flush().catchError((_) {}));

      // 轮转文件
      if (_currentSize >= _maxFileSize) {
        _rotateFiles();
      }
    } catch (e) {
      // sink 已关闭或异常，重置
      _sink = null;
    }
  }

  Future<void> _rotateFiles() async {
    await _sink?.flush();
    await _sink?.close();

    // 删除最旧的文件
    final files = _logDir!
        .listSync()
        .whereType<File>()
        .where((f) => f.path.endsWith('.log'))
        .toList();
    files.sort((a, b) => a.lastModifiedSync().compareTo(b.lastModifiedSync()));

    while (files.length >= _maxFiles) {
      await files.first.delete();
      files.removeAt(0);
    }

    await _openFile();
  }

  Future<void> close() async {
    await _sink?.flush();
    await _sink?.close();
  }
}

final _fileWriter = _FileLogWriter();

/// 当前 Talker 实例引用（用于运行时切换日志等级）
Talker? _talker;

/// 运行时切换日志等级（立即生效，无需重启）
/// 通过 configure 整体替换 logger（TalkerLoggerSettings.level 为 final，
/// 无法原地修改），observer/history 会被保留。
void setLogLevel(LogLevel level) {
  final talker = _talker;
  if (talker == null) return;
  talker.configure(
    logger: TalkerLogger(
      settings: TalkerLoggerSettings(level: level),
      formatter: CustomLoggerFormatter(),
    ),
  );
  talker.info('log level changed to ${level.name}');
}

/// 文件日志观察者
class _FileLogObserver extends TalkerObserver {
  @override
  void onError(TalkerError err) {
    _fileWriter.write('[ERROR] ${err.message}');
  }

  @override
  void onException(TalkerException err) {
    _fileWriter.write('[EXCEPTION] ${err.message}');
  }

  @override
  void onLog(TalkerData log) {
    _fileWriter.write('[${log.logLevel?.name ?? "LOG"}] ${log.message}');
  }
}

Future<void> initTalkerLogger({
  bool isBackground = false,
  String? logDirPath,
  LogLevel? logLevel,
}) async {
  // 初始化日志目录
  Directory logDir;
  if (logDirPath != null) {
    logDir = Directory(logDirPath);
  } else {
    final appDir = await getApplicationSupportDirectory();
    logDir = Directory('${appDir.path}/logs');
  }
  _logDir = logDir;
  if (!await _logDir!.exists()) {
    await _logDir!.create(recursive: true);
  }

  // 初始化文件写入器
  await _fileWriter.init(_logDir!);

  // 日志等级：优先使用调用方传入的配置（持久化的用户设置），
  // 否则 debug 构建默认 debug，release 构建默认 info
  final level = logLevel ?? (kDebugMode ? LogLevel.debug : LogLevel.info);

  // 后台隔离进程使用简单的 Talker，避免依赖 Flutter 插件
  final talker = isBackground
      ? Talker(
          logger: TalkerLogger(
            settings: TalkerLoggerSettings(level: level),
          ),
          settings: TalkerSettings(),
          observer: _FileLogObserver(),
        )
      : TalkerFlutter.init(
          logger: TalkerLogger(
            settings: TalkerLoggerSettings(
              level: level,
            ),
            formatter: CustomLoggerFormatter(),
          ),
          settings: TalkerSettings(
            colors: <String, AnsiPen>{TalkerKey.debug: AnsiPen()..magenta()},
          ),
          observer: _FileLogObserver(),
        );

  _talker = talker;
  GetIt.I.registerSingleton<Talker>(talker);
  talker.info(
    'talker init complete (background=$isBackground), log dir: ${_logDir!.path}',
  );
}

/// 获取日志文件列表（用于导出）
Future<List<File>> getLogFiles() async {
  if (_logDir == null || !await _logDir!.exists()) {
    return [];
  }
  final files = _logDir!
      .listSync()
      .whereType<File>()
      .where((f) => f.path.endsWith('.log'))
      .toList();
  files.sort((a, b) => b.lastModifiedSync().compareTo(a.lastModifiedSync()));
  return files;
}

/// 分享日志文件
Future<void> shareLogFiles() async {
  final files = await getLogFiles();
  if (files.isEmpty) return;

  final xFiles = files.map((f) => XFile(f.path)).toList();
  await SharePlus.instance.share(
    ShareParams(files: xFiles, subject: 'PFM 日志文件'),
  );
}

/// 清理旧日志
Future<void> cleanupOldLogs({int keepDays = 7}) async {
  if (_logDir == null || !await _logDir!.exists()) return;

  final cutoff = DateTime.now().subtract(Duration(days: keepDays));
  final files = _logDir!.listSync().whereType<File>();

  for (final file in files) {
    if (file.lastModifiedSync().isBefore(cutoff)) {
      await file.delete();
    }
  }
}

/// 原生日志目录（Kotlin/ObjC 写入）：Android/iOS 为
/// getApplicationSupportDirectory()/pfm_logs（Android 上即 filesDir），
/// 桌面平台原生侧无此目录，返回 null。
Future<Directory?> getNativeLogDir() async {
  if (!Platform.isAndroid && !Platform.isIOS) return null;
  try {
    final supportDir = await getApplicationSupportDirectory();
    return Directory('${supportDir.path}/pfm_logs');
  } catch (_) {
    return null;
  }
}

/// 获取原生日志文件列表（按修改时间倒序）
Future<List<File>> getNativeLogFiles() async {
  final dir = await getNativeLogDir();
  if (dir == null || !await dir.exists()) return [];
  try {
    final files = dir
        .listSync()
        .whereType<File>()
        .where((f) => f.path.endsWith('.log'))
        .toList();
    files.sort((a, b) => b.lastModifiedSync().compareTo(a.lastModifiedSync()));
    return files;
  } catch (_) {
    return [];
  }
}

/// 获取全部日志文件（Flutter 日志 + 原生日志），按修改时间倒序
Future<List<File>> getAllLogFiles() async {
  final flutterFiles = await getLogFiles();
  final nativeFiles = await getNativeLogFiles();
  final all = [...flutterFiles, ...nativeFiles];
  all.sort((a, b) => b.lastModifiedSync().compareTo(a.lastModifiedSync()));
  return all;
}

/// 清空全部日志：删除 Flutter 与原生日志文件，并清空当前内存日志
Future<void> clearAllLogs() async {
  final files = await getAllLogFiles();
  for (final f in files) {
    try {
      await f.delete();
    } catch (_) {}
  }
  try {
    getLogger().cleanHistory();
  } catch (_) {}
}

/// 自定义日志格式化器
class CustomLoggerFormatter extends LoggerFormatter {
  @override
  String fmt(LogDetails details, TalkerLoggerSettings settings) {
    final underline = ConsoleUtils.getUnderline(
      settings.maxLineWidth,
      lineSymbol: settings.lineSymbol,
      withCorner: true,
    );
    final topline = ConsoleUtils.getTopline(
      settings.maxLineWidth,
      lineSymbol: settings.lineSymbol,
      withCorner: true,
    );
    final msg = details.message?.toString() ?? '';
    var msgBorderedLines = msg.split('\n').map((e) => '│ $e');

    // 添加调用者信息
    final callerInfo = _getCallerInfo(StackTrace.current);
    final linesList = msgBorderedLines.toList();
    if (linesList.isNotEmpty) {
      linesList[0] = '│ $callerInfo ${linesList[0].substring(2)}';
    }
    msgBorderedLines = linesList;

    if (!settings.enableColors) {
      return '$topline\n${msgBorderedLines.join('\n')}\n$underline';
    }
    var lines = [topline, ...msgBorderedLines, underline];
    lines = lines.map((e) => details.pen.write(e)).toList();
    final coloredMsg = lines.join('\n');
    return coloredMsg;
  }

  String _getCallerInfo(StackTrace stackTrace) {
    try {
      final frames = stackTrace.toString().split('\n');
      for (final frame in frames) {
        if (frame.contains('.dart') &&
            !frame.contains('package:talker') &&
            !frame.contains('services/logger.dart')) {
          final pattern = RegExp(r'package:[^/]+/([^:]+):(\d+):(\d+)');
          final match = pattern.firstMatch(frame);
          if (match != null) {
            final file = match.group(1);
            final line = match.group(2);
            return '$file:$line';
          }
        }
      }
    } catch (e) {
      // ignore errors
    }
    return 'unknown:0';
  }
}
