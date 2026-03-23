import 'dart:async';
import 'dart:io' as io;
import 'dart:isolate';
import 'dart:math';
import 'dart:typed_data';

// 直接导入 blake3_dart 内部类以支持增量哈希
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/proto/native/enum.pb.dart';
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

  IsolateConfig({
    required this.id,
    required this.sendPort,
    required this.baseUrl,
    required this.token,
    required this.dbPath,
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

Future<void> uploadFile({
  required String taskId,
  required Sqlite3Storage storage,
  required FmRepository fmRepo,
}) async {
  // 获取任务记录
  final task = await storage.getUploadTaskAsync(taskId);
  if (task == null) {
    getLogger().warning('upload record not found for taskId: $taskId');
    return;
  }
  // 计算文件哈希
  if (task.fileHash == null || task.fileHash!.isEmpty) {
    await storage.updateUploadTask(
      task.id,
      status: UploadStatus.UPLOAD_STATUS_HASHING,
    );
    final fileHash = await _computeFileHash(task.filePath);
    await storage.updateUploadTask(task.id, fileHash: fileHash);
    task.fileHash = fileHash;
  }

  // 更新状态为上传中
  await storage.updateUploadTask(
    task.id,
    status: UploadStatus.UPLOAD_STATUS_UPLOADING,
  );
  List<int> uploadedChunks = [];
  // 初始化上传
  if (task.uploadId == null || task.uploadId!.isEmpty) {
    final result = await fmRepo.initUpload(
      name: task.fileName,
      size: task.fileSize,
      hash: task.fileHash!,
      parentId: task.parentId,
    );
    if (!result.success) {
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.UPLOAD_STATUS_FAILED,
        errorMessage: result.message,
      );
      getLogger().error('InitUpload failed: ${result.message}');
      return;
    }
    final data = result.data!;

    final uploadId = data.uploadId;
    getLogger().info(
      'InitUpload result: uploadId=$uploadId, '
      'chunkTotal=${data.chunkTotal}, chunkSize=${data.chunkSize}, '
      'exists=${data.exists}, uploadedChunks=${data.uploadedChunks}',
    );

    // 检查是否秒传（exists 表示文件已存在）
    if (data.exists) {
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.UPLOAD_STATUS_COMPLETED,
      );
      return;
    }
    task.uploadId = uploadId;
    task.chunkTotal = data.chunkTotal;
    task.chunkSize = data.chunkSize.toInt();

    // 获取已上传的分片
    if (data.uploadedChunks.isNotEmpty) {
      await storage.updateUploadTask(
        task.id,
        uploadId: uploadId,
        chunkTotal: data.chunkTotal,
        chunkSize: data.chunkSize,
      );
      uploadedChunks = data.uploadedChunks;
    }
  }

  // TODO： 重构下面的代码，不要动上面的
  // task.uploadedChunks现在是位图格式，位置一表示当前分片已上传，从0开始
  // 并发上传太复杂了，请修改成单线程上传
  // 引入了FmRepository实例，请使用里面的接口进行上传
  // task里面新增了uploadedBytes字段，请配合uploadChunk接口里面的onProgress参数进行速度和进度计算
  // 上传期间，每秒记录一次uploadedBytes和speed字段，并更新到数据库

  // 单线程顺序上传分片
  final file = io.File(task.filePath);
  final totalChunks = task.chunkTotal;
  final chunkSize = task.chunkSize;
  final bitmap = uploadedChunks;

  // 已上传字节数（从task中恢复）
  int uploadedBytes = task.uploadedBytes;

  // 实际上传字节数，如果某个分片上传失败，之前的上传字节数会被重置，所以需要一个实际的上传字节数
  int realUploadedBytes = 0;

  // 用于计算速度的字节差值
  int deltaUploadedBytes = 0;

  // 获取待上传的分片列表（从位图中查找未上传的分片）
  final pendingChunks = <int>[];
  for (int i = 0; i < totalChunks; i++) {
    final byteIndex = i ~/ 8;
    final bitIndex = i % 8;
    if (byteIndex >= bitmap.length ||
        (bitmap[byteIndex] & (1 << bitIndex)) == 0) {
      pendingChunks.add(i);
    } else {
      // 根据已上传的分片计算实际上传的字节数
      realUploadedBytes += i == totalChunks - 1
          ? task.fileSize - chunkSize * (totalChunks - 1)
          : chunkSize;
    }
  }

  if (pendingChunks.isNotEmpty) {
    //执行上传
    getLogger().info(
      'Starting single-threaded chunk upload: ${pendingChunks.length} chunks to upload',
    );

    final raf = await file.open();
    final timer = Timer.periodic(Duration(seconds: 1), (t) {
      // 定期更新速度
      storage.updateUploadTask(
        task.id,
        speed: deltaUploadedBytes,
        uploadedBytes: max(realUploadedBytes, uploadedBytes),
      );
      deltaUploadedBytes = 0;
    });
    try {
      for (final chunkIndex in pendingChunks) {
        // 检查是否被取消或暂停
        final currentTask = await storage.getUploadTaskAsync(task.id);
        if (currentTask?.status == UploadStatus.UPLOAD_STATUS_CANCELED ||
            currentTask?.status == UploadStatus.UPLOAD_STATUS_PAUSED) {
          getLogger().info('Upload cancelled or paused, stopping upload');
          return;
        }

        // 读取分片数据
        final offset = chunkIndex * chunkSize;
        final remainingBytes = task.fileSize - offset;
        final currentChunkSize = remainingBytes > chunkSize
            ? chunkSize
            : remainingBytes;

        await raf.setPosition(offset);
        final chunkData = await raf.read(currentChunkSize);

        // 计算分片哈希
        final chunkHash = _computeBlake3Hash(chunkData);

        // 上传分片
        final result = await fmRepo.uploadChunk(
          uploadId: task.uploadId!,
          chunkIndex: chunkIndex,
          chunkData: chunkData,
          chunkHash: chunkHash,
          onSendProgress: (sent, total) {
            realUploadedBytes += sent;
            deltaUploadedBytes += sent;
          },
        );

        if (!result.success) {
          await storage.updateUploadTask(
            task.id,
            status: UploadStatus.UPLOAD_STATUS_FAILED,
            errorMessage: result.message,
          );
          getLogger().error(
            'Chunk $chunkIndex upload failed: ${result.message}',
          );
          // 单线程模式遇到错误直接停止
          return;
        }
      }
    } catch (e) {
      getLogger().error('Chunk upload failed: $e');
      await storage.updateUploadTask(
        task.id,
        status: UploadStatus.UPLOAD_STATUS_FAILED,
        errorMessage: e.toString(),
      );
      return;
    } finally {
      timer.cancel();
      raf.close();
    }
  }

  // 4. 完成上传
  final result = await fmRepo.completeUpload(uploadId: task.uploadId!);
  if (!result.success) {
    await storage.updateUploadTask(
      task.id,
      status: UploadStatus.UPLOAD_STATUS_FAILED,
      errorMessage: result.message,
    );
    getLogger().error('CompleteUpload failed: ${result.message}');
    return;
  }

  await storage.updateUploadTask(
    task.id,
    status: UploadStatus.UPLOAD_STATUS_COMPLETED,
  );

  getLogger().info('Upload completed successfully');
}

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
  // 通知主线程Isolate已准备就绪
  sendPort.send(
    IsolateMessage(
      isolateId: config.id,
      taskType: TaskType.upload,
      sendPort: receivePort.sendPort,
      status: IsolateStatus.ready,
    ),
  );

  receivePort.listen((message) async {
    final msg = message as IsolateCommand;
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
      } else {
        // TODO：下载文件
      }
    } catch (e) {
      getLogger().error('任务执行失败，taskId:${msg.taskId},error:$e');
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
}

/// 计算文件 Hash（使用 Blake3）
Future<String> _computeFileHash(String filePath) async {
  final file = io.File(filePath);
  final hasher = HashContext.unkeyed();

  getLogger().debug(
    'start hashing file ${DateTime.now().millisecondsSinceEpoch}',
  );
  await for (final chunk in file.openRead()) {
    hasher.update(Uint8List.fromList(chunk));
  }
  getLogger().debug(
    'end hashing file ${DateTime.now().millisecondsSinceEpoch}',
  );

  final output = Uint8List(32);
  hasher.finalize(output);
  return output.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

/// 计算数据的 Blake3 哈希
String _computeBlake3Hash(List<int> data) {
  return blake3Hex(Uint8List.fromList(data));
}
