import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/format.dart';

/// 上传任务持久化记录
///
/// 纯 Dart 模型类，支持 SQLite 存储
class UploadTaskRecord {
  String id;
  // 文件本地路径
  String filePath;
  String fileName;
  int fileSize;
  // 文件父ID，服务器端父目录ID
  String parentId;
  // Blake3 哈希值
  String? fileHash;
  int chunkSize;
  int chunkTotal;
  // 任务状态
  UploadStatus status;
  String? errorMessage;
  String? uploadId;
  DateTime createdAt;
  DateTime? completedAt;
  int retryCount;
  int uploadedBytes; // 已上传字节数
  int speed; // 上传速度 (字节）

  UploadTaskRecord({
    required this.id,
    required this.filePath,
    required this.fileName,
    required this.fileSize,
    required this.parentId,
    this.fileHash,
    required this.chunkSize,
    required this.chunkTotal,
    this.status = UploadStatus.UPLOAD_STATUS_PENDING,
    this.errorMessage,
    this.uploadId,
    DateTime? createdAt,
    this.completedAt,
    this.uploadedBytes = 0,
    this.speed = 0,
    this.retryCount = 0,
  }) : createdAt = createdAt ?? DateTime.now();

  /// 上传进度（0-100）
  double get progress {
    if (chunkTotal == 0) return 0;
    final currentProgress =
        (uploadedBytes >= fileSize ? 1.0 : uploadedBytes / fileSize) * 100;
    return currentProgress;
  }

  String get formatSpeed {
    return '${formatCapacityString(speed)}/s';
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'filePath': filePath,
      'fileName': fileName,
      'fileSize': fileSize,
      'parentId': parentId,
      'fileHash': fileHash,
      'chunkSize': chunkSize,
      'chunkTotal': chunkTotal,
      'status': status.value,
      'errorMessage': errorMessage,
      'uploadId': uploadId,
      'createdAt': createdAt.toIso8601String(),
      'completedAt': completedAt?.toIso8601String(),
      'retryCount': retryCount,
      'uploadedBytes': uploadedBytes,
      'speed': speed,
    };
  }

  static UploadTaskRecord fromJson(Map<String, dynamic> json) {
    return UploadTaskRecord(
      id: json['id'],
      filePath: json['filePath'],
      fileName: json['fileName'],
      fileSize: json['fileSize'],
      parentId: json['parentId'],
      fileHash: json['fileHash'],
      chunkSize: json['chunkSize'],
      chunkTotal: json['chunkTotal'],
      status: json['status'] != null
          ? UploadStatus.values[json['status']]
          : UploadStatus.UPLOAD_STATUS_PENDING,
      errorMessage: json['errorMessage'],
      uploadId: json['uploadId'],
      createdAt: json['createdAt'] != null
          ? DateTime.parse(json['createdAt'])
          : DateTime.now(),
      completedAt: json['completedAt'] != null
          ? DateTime.parse(json['completedAt'])
          : null,
      retryCount: json['retryCount'] ?? 0,
      uploadedBytes: json['uploadedBytes'] ?? 0,
      speed: json['speed'] ?? 0,
    );
  }

  static String createSql() {
    return '''
      CREATE TABLE IF NOT EXISTS upload_tasks (
        id TEXT PRIMARY KEY,
        filePath TEXT,
        fileName TEXT,
        fileSize INTEGER,
        parentId TEXT,
        fileHash TEXT,
        chunkSize INTEGER,
        chunkTotal INTEGER,
        status INTEGER,
        errorMessage TEXT,
        uploadId TEXT,
        createdAt TEXT,
        completedAt TEXT,
        retryCount INTEGER,
        uploadedBytes INTEGER,
        speed INTEGER
      );
      CREATE INDEX idx_upload_tasks_status ON ${Sqlite3Storage.uploadTaskTableName} (status);
      CREATE INDEX idx_upload_tasks_created_at ON ${Sqlite3Storage.uploadTaskTableName} (createdAt);
    ''';
  }
}
