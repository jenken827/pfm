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
    _currentSize = await _currentFile!.length();
  }

  void write(String message) {
    if (_sink == null) return;

    try {
      final timestamp = DateTime.now().toString().substring(11, 23);
      _sink!.writeln('$timestamp $message');
      _currentSize += message.length + 1;

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

Future<void> initTalkerLogger({bool isBackground = false, String? logDirPath}) async {
  // 初始化日志目录
  Directory logDir;
  if (logDirPath != null) {
    logDir = Directory(logDirPath);
  } else {
    final appDir = await getApplicationDocumentsDirectory();
    logDir = Directory('${appDir.path}/logs');
  }
  _logDir = logDir;
  if (!await _logDir!.exists()) {
    await _logDir!.create(recursive: true);
  }

  // 初始化文件写入器
  await _fileWriter.init(_logDir!);

  // 后台隔离进程使用简单的 Talker，避免依赖 Flutter 插件
  final talker = isBackground
      ? Talker(
          logger: TalkerLogger(
            settings: TalkerLoggerSettings(
              level: LogLevel.info,
            ),
          ),
          settings: TalkerSettings(),
          observer: _FileLogObserver(),
        )
      : TalkerFlutter.init(
          logger: TalkerLogger(
            settings: TalkerLoggerSettings(
              level: kDebugMode ? LogLevel.debug : LogLevel.info,
            ),
            formatter: CustomLoggerFormatter(),
          ),
          settings: TalkerSettings(
            colors: <String, AnsiPen>{TalkerKey.debug: AnsiPen()..magenta()},
          ),
          observer: _FileLogObserver(),
        );

  GetIt.I.registerSingleton<Talker>(talker);
  talker.info('talker init complete (background=$isBackground), log dir: ${_logDir!.path}');
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
  await Share.shareXFiles(xFiles, subject: 'PFM 日志文件');
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
