import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/format.dart';

/// 上传任务持久化记录
///
/// 纯 Dart 模型类，支持 SQLite 存储
class UploadTaskRecord {
  String id;
  // 文件本地路径
  String filepath;
  String filename;
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
  // 文件标签（可选）
  String? tags;
  // 是否覆盖同名文件
  bool overwrite;

  UploadTaskRecord({
    required this.id,
    required this.filepath,
    required this.filename,
    required this.fileSize,
    required this.parentId,
    this.fileHash,
    this.chunkSize = 0,
    this.chunkTotal = 0,
    this.status = UploadStatus.PENDING,
    this.errorMessage,
    this.uploadId,
    DateTime? createdAt,
    this.completedAt,
    this.uploadedBytes = 0,
    this.speed = 0,
    this.retryCount = 0,
    this.tags,
    this.overwrite = false,
  }) : createdAt = createdAt ?? DateTime.now();

  /// 上传进度（0-100）
  double get progress {
    if (fileSize == 0) return 0;

    // 失败或取消时，返回上传过程中的实际进度（可能不是100%）
    if (status == UploadStatus.FAILED || status == UploadStatus.CANCELED) {
      return (uploadedBytes / fileSize) * 100;
    }

    // 完成时返回100%
    if (status == UploadStatus.COMPLETED) {
      return 100;
    }

    // 其他状态返回当前进度
    final currentProgress =
        (uploadedBytes >= fileSize ? 1.0 : uploadedBytes / fileSize) * 100;
    return currentProgress;
  }

  bool get isActive =>
      status == UploadStatus.PENDING ||
      status == UploadStatus.UPLOADING ||
      status == UploadStatus.HASHING;

  String get formatSpeed {
    return '${formatCapacityString(speed)}/s';
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'filepath': filepath,
      'filename': filename,
      'file_size': fileSize,
      'parent_id': parentId,
      'file_hash': fileHash,
      'chunk_size': chunkSize,
      'chunk_total': chunkTotal,
      'status': status.value,
      'error_message': errorMessage,
      'upload_id': uploadId,
      'created_at': createdAt.toIso8601String(),
      'completed_at': completedAt?.toIso8601String(),
      'retry_count': retryCount,
      'uploaded_bytes': uploadedBytes,
      'speed': speed,
      'tags': tags,
      'overwrite': overwrite ? 1 : 0,
    };
  }

  static UploadTaskRecord fromJson(Map<String, dynamic> json) {
    return UploadTaskRecord(
      id: json['id'],
      filepath: json['filepath'],
      filename: json['filename'],
      fileSize: json['file_size'],
      parentId: json['parent_id'],
      fileHash: json['file_hash'],
      chunkSize: json['chunk_size'],
      chunkTotal: json['chunk_total'],
      status: json['status'] != null
          ? UploadStatus.values[json['status']]
          : UploadStatus.PENDING,
      errorMessage: json['error_message'],
      uploadId: json['upload_id'],
      createdAt: json['created_at'] != null
          ? DateTime.parse(json['created_at'])
          : DateTime.now(),
      completedAt: json['completed_at'] != null
          ? DateTime.parse(json['completed_at'])
          : null,
      retryCount: json['retry_count'] ?? 0,
      uploadedBytes: json['uploaded_bytes'] ?? 0,
      speed: json['speed'] ?? 0,
      tags: json['tags'],
      overwrite: json['overwrite'] == 1,
    );
  }

  static String createSql() {
    return '''
      CREATE TABLE IF NOT EXISTS upload_tasks (
        id TEXT PRIMARY KEY,
        filepath TEXT,
        filename TEXT,
        file_size INTEGER,
        parent_id TEXT,
        file_hash TEXT,
        chunk_size INTEGER,
        chunk_total INTEGER,
        status INTEGER,
        error_message TEXT,
        upload_id TEXT,
        created_at TEXT,
        completed_at TEXT,
        retry_count INTEGER,
        uploaded_bytes INTEGER,
        speed INTEGER,
        tags TEXT,
        overwrite INTEGER
      );
      CREATE INDEX idx_ut_status ON ${Sqlite3Storage.uploadTaskTableName} (status);
      CREATE INDEX idx_ut_created_at ON ${Sqlite3Storage.uploadTaskTableName} (created_at);
    ''';
  }
}
