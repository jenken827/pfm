import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:talker_flutter/talker_flutter.dart';

Talker getLogger() => GetIt.I.get<Talker>();

void initTalkerLogger() {
  final talker = TalkerFlutter.init(
    logger: TalkerLogger(
      settings: TalkerLoggerSettings(
        level: kDebugMode ? LogLevel.debug : LogLevel.info,
      ),
      formatter: CustomLoggerFormatter(),
    ),
    settings: TalkerSettings(
      colors: <String, AnsiPen>{TalkerKey.debug: AnsiPen()..magenta()},
    ),
  );
  GetIt.I.registerSingleton<Talker>(talker);
  talker.info('talker init complete');
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
