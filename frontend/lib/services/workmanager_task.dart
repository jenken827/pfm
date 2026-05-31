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
  final String taskId; //任务id
  final TaskType taskType; //上传还是下载
  final String baseUrl; //服务器地址
  final CasdoorToken token; //令牌
  final String dbPath; //数据库路径

  WmTaskParam({
    required this.taskId,
    required this.taskType,
    required this.baseUrl,
    required this.token,
    required this.dbPath,
  });

  static WmTaskParam fromJson(Map<String, dynamic> json) {
    return WmTaskParam(
      taskId: json['taskId'] as String,
      taskType: json['taskType'] == 0 ? TaskType.upload : TaskType.download,
      baseUrl: json['baseUrl'] as String,
      token: CasdoorToken.fromMap(jsonDecode(json['token'])),
      dbPath: json['dbPath'] as String,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'taskId': taskId,
      'taskType': taskType.index,
      'baseUrl': baseUrl,
      'token': jsonEncode(token.toJson()),
      'dbPath': dbPath,
    };
  }
}

/// WorkManager 后台任务处理

/// 后台任务回调调度器
///
/// 注意：此函数在 WorkManager 的后台隔离环境中执行，
/// 不能使用 UI 线程的 GetIt 实例或任何依赖它的对象
@pragma('vm:entry-point')
void callbackDispatcher() {
  // 仅在移动平台执行
  if (kIsWeb) return;

  // ignore: avoid_print
  print('[WorkManager] callbackDispatcher called!');

  Workmanager().executeTask((taskName, inputData) async {
    // ignore: avoid_print
    print('[WorkManager] executeTask called: taskName=$taskName, inputData=$inputData');

    if (inputData == null) {
      // ignore: avoid_print
      print('[WorkManager] inputData is null, returning true');
      return Future.value(true);
    }

    try {
      final params = WmTaskParam.fromJson(inputData);
      // ignore: avoid_print
      print('[WorkManager] Parsed params: taskId=${params.taskId}, taskType=${params.taskType}, baseUrl=${params.baseUrl}');

      // 预先初始化后台 Logger 单例
      initTalkerLogger();

      // 初始化存储
      // ignore: avoid_print
      print('[WorkManager] Initializing storage with dbPath: ${params.dbPath}');
      final storage = await Sqlite3Storage.initForIsolate(params.dbPath);
      // ignore: avoid_print
      print('[WorkManager] Storage initialized');

      if (params.taskType == TaskType.upload) {
        // ignore: avoid_print
        print('[WorkManager] Starting upload task: ${params.taskId}');
        final fmRepo = FmRepository.isolateInit(params.baseUrl, params.token);
        // ignore: avoid_print
        print('[WorkManager] FmRepository initialized, starting uploadFile');
        await uploadFile(taskId: params.taskId, storage: storage, fmRepo: fmRepo);
        // ignore: avoid_print
        print('[WorkManager] uploadFile completed for task: ${params.taskId}');
      } else if (params.taskType == TaskType.download) {
        final task = await storage.getDownloadTaskAsync(params.taskId);
        if (task != null) {
          await downloadFile(
            taskId: params.taskId,
            storage: storage,
            baseUrl: params.baseUrl,
            token: params.token,
          );
        }
      }
      return Future.value(true);
    } catch (e, stackTrace) {
      // ignore: avoid_print
      print('[WorkManager] Error in executeTask: $e');
      // ignore: avoid_print
      print('[WorkManager] Stack trace: $stackTrace');
      return Future.value(false);
    }
  });
}
