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
      token: jsonDecode(json['token']) as CasdoorToken,
      dbPath: json['dbPath'] as String,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'taskId': taskId,
      'taskType': taskType.index,
      'baseUrl': baseUrl,
      'token': jsonEncode(token),
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

  Workmanager().executeTask((taskName, inputData) async {
    if (inputData == null) {
      return Future.value(true);
    }
    final params = WmTaskParam.fromJson(inputData);

    // 预先初始化后台 Logger 单例
    initTalkerLogger();

    // 初始化存储
    final storage = await Sqlite3Storage.initForIsolate(params.dbPath);

    if (params.taskType == TaskType.upload) {
      final fmRepo = FmRepository.isolateInit(params.baseUrl, params.token);
      await uploadFile(taskId: params.taskId, storage: storage, fmRepo: fmRepo);
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
  });
}
