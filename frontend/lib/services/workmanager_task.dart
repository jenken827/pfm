import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';

// 条件导入 WorkManager（仅移动平台）
import 'package:workmanager/workmanager.dart'
    if (dart.library.html) ''
    if (dart.library.io) 'package:workmanager/workmanager.dart';

class WmTaskParam {
  final String taskId;
  final TaskType taskType;
  final String baseUrl;
  final CasdoorToken token;
  final String dbPath;
  final int chunkSize;
  final int smallFileThreshold;

  WmTaskParam({
    required this.taskId,
    required this.taskType,
    required this.baseUrl,
    required this.token,
    required this.dbPath,
    this.chunkSize = 2 * 1024 * 1024,
    this.smallFileThreshold = 5 * 1024 * 1024,
  });

  static WmTaskParam fromJson(Map<String, dynamic> json) {
    return WmTaskParam(
      taskId: json['taskId'] as String,
      taskType: json['taskType'] == 0 ? TaskType.upload : TaskType.download,
      baseUrl: json['baseUrl'] as String,
      token: CasdoorToken.fromMap(jsonDecode(json['token'])),
      dbPath: json['dbPath'] as String,
      chunkSize: json['chunkSize'] as int? ?? 2 * 1024 * 1024,
      smallFileThreshold: json['smallFileThreshold'] as int? ?? 5 * 1024 * 1024,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'taskId': taskId,
      'taskType': taskType.index,
      'baseUrl': baseUrl,
      'token': jsonEncode(token.toJson()),
      'dbPath': dbPath,
      'chunkSize': chunkSize,
      'smallFileThreshold': smallFileThreshold,
    };
  }
}

/// WorkManager 后台任务回调调度器
@pragma('vm:entry-point')
void callbackDispatcher() {
  if (kIsWeb) return;

  Workmanager().executeTask((taskName, inputData) async {
    if (inputData == null) {
      return Future.value(true);
    }

    try {
      final params = WmTaskParam.fromJson(inputData);
      initTalkerLogger(isBackground: true);
      final logger = getLogger();
      logger.info('[WorkManager] Processing task: ${params.taskId} (${params.taskType})');

      final storage = await Sqlite3Storage.initForIsolate(params.dbPath);

      if (params.taskType == TaskType.upload) {
        final fmRepo = FmRepository.isolateInit(params.baseUrl, params.token);
        await uploadFile(
          taskId: params.taskId,
          storage: storage,
          fmRepo: fmRepo,
          chunkSize: params.chunkSize,
          smallFileThreshold: params.smallFileThreshold,
        );
        logger.info('[WorkManager] Upload completed: ${params.taskId}');
      } else if (params.taskType == TaskType.download) {
        final task = await storage.getDownloadTaskAsync(params.taskId);
        if (task != null) {
          await downloadFile(
            taskId: params.taskId,
            storage: storage,
            baseUrl: params.baseUrl,
            token: params.token,
          );
          logger.info('[WorkManager] Download completed: ${params.taskId}');
        }
      }
      return Future.value(true);
    } catch (e, stackTrace) {
      getLogger().error('[WorkManager] Task failed: $e\n$stackTrace');
      return Future.value(false);
    }
  });
}
