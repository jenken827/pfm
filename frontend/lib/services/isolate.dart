import 'dart:isolate';

import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';

/// Isolate会复用，通过命令触发执行上传任务或下载任务
/// 其它信息是通过共享sqlite3数据源同步的，所以信息传递不会那么实时

// Isolate 状态, 初始化中，准备就绪，执行中，本次完成
// 只有准备就绪和本次完成的Isolate才会立即响应命令，否则会队列等待
enum IsolateStatus { initializing, ready, executing, completed }

// Isolate 信息
class IsolateInfo {
  final Isolate i;
  SendPort? sendPort; //发送端口，用于向Isolate发送命令
  final ReceivePort receivePort; //接收端口，用于接收Isolate的响应，比如任务执行完毕
  IsolateStatus status; // Isolate 状态，用于标记当前Isolate是否可用
  // String? currentTaskId; //当前任务id

  IsolateInfo({
    required this.i,
    this.sendPort,
    required this.receivePort,
    this.status = IsolateStatus.initializing,
    // this.currentTaskId,
  });
}

/// Isolate 配置数据
class IsolateConfig {
  final SendPort sendPort; //isolate向外部发送消息的端口
  final String baseUrl;
  final CasdoorToken token;
  final String dbPath;
  final String id;
  final TaskType taskType;

  IsolateConfig({
    required this.id,
    required this.sendPort,
    required this.baseUrl,
    required this.token,
    required this.dbPath,
    required this.taskType,
  });
}

enum TaskType { upload, download }

// Isolate 消息类。Isolate发送给外部接收端的消息
class IsolateMessage {
  final String isolateId;
  final TaskType taskType;
  final IsolateStatus status;
  final SendPort? sendPort; //外部向Isolate发送消息的端口

  IsolateMessage({
    required this.isolateId,
    required this.taskType,
    required this.status,
    this.sendPort,
  });
}

class IsolateCommand {
  final TaskType taskType;
  final String taskId;

  IsolateCommand({required this.taskType, required this.taskId});
}

// 处理接收到的命令
void handleCommandsToIsolate(dynamic message, SendPort sendPort) {}

/// Isolate 入口点（桌面平台）
@pragma('vm:entry-point')
void isolateEntryPoint(IsolateConfig config) async {
  // 预先初始化后台 Logger 单例
  initTalkerLogger();

  final sendPort = config.sendPort;
  // 初始化存储
  final storage = await Sqlite3Storage.initForIsolate(config.dbPath);

  final fmRepo = FmRepository.isolateInit(config.baseUrl, config.token);

  // 接收端口，监听消息
  final receivePort = ReceivePort();

  final logger = getLogger();

  logger.debug('isolate start listen to main isolate command ---');

  receivePort.listen((message) async {
    final msg = message as IsolateCommand;
    logger.debug(
      'isolate receive command: tasktype: ${msg.taskType}, taskid: ${msg.taskId}',
    );
    try {
      // 修改状态为执行中
      sendPort.send(
        IsolateMessage(
          status: IsolateStatus.executing,
          isolateId: config.id,
          taskType: msg.taskType,
        ),
      );
      // 判断任务类型，执行对应的方法
      if (msg.taskType == TaskType.upload) {
        await uploadFile(taskId: msg.taskId, storage: storage, fmRepo: fmRepo);
      } else if (msg.taskType == TaskType.download) {
        await downloadFile(
          taskId: msg.taskId,
          storage: storage,
          baseUrl: config.baseUrl,
          token: config.token,
        );
      }
    } catch (e, s) {
      getLogger().error('任务执行失败，taskId:${msg.taskId},error:$e,$s');
    } finally {
      sendPort.send(
        IsolateMessage(
          status: IsolateStatus.completed,
          isolateId: config.id,
          taskType: msg.taskType,
        ),
      );
    }
  });

  logger.debug('send message to main isolate, ready to receive command---');
  // 通知主线程Isolate已准备就绪
  sendPort.send(
    IsolateMessage(
      isolateId: config.id,
      taskType: config.taskType,
      sendPort: receivePort.sendPort,
      status: IsolateStatus.ready,
    ),
  );
}
