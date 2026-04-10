import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/services.dart';
import 'package:pfm/utils/format.dart';

/// 下载状态枚举（用于持久化存储）
enum DownloadStatus {
  PENDING,       // 待开始
  DOWNLOADING,   // 下载中
  COMPLETED,     // 已完成
  FAILED,        // 失败
  CANCELED,      // 已取消
  PAUSED,        // 已暂停
  PRE_DOWNLOAD,  // 预下载中（Telegram 大文件等待缓存）
}

/// 下载任务持久化记录
///
/// 纯 Dart 模型类，支持 SQLite 存储
class DownloadTaskRecord {
  String id;
  // 服务端文件ID
  String fileId;
  String filename;
  int fileSize;
  // Blake3 哈希值（用于校验）
  String? fileHash;
  // 已下载字节数（用于断点续传）
  int downloadedBytes;
  // 下载速度 (字节/秒)
  int speed;
  // 任务状态
  DownloadStatus status;
  String? errorMessage;
  // 本地保存路径
  String savePath;
  // 分片大小（默认 5MB）
  int chunkSize;
  // 总分片数
  int chunkTotal;
  // 并发下载线程数
  int workerCount;
  /// 指定下载的存储提供者 ID（可选，为空则由后端自动选择）
  int? providerId;
  DateTime createdAt;
  DateTime? completedAt;

  DownloadTaskRecord({
    required this.id,
    required this.fileId,
    required this.filename,
    required this.fileSize,
    this.fileHash,
    this.downloadedBytes = 0,
    this.speed = 0,
    this.status = DownloadStatus.PENDING,
    this.errorMessage,
    required this.savePath,
    this.chunkSize = 5 * 1024 * 1024, // 默认 5MB
    this.chunkTotal = 0,
    this.workerCount = 3,
    this.providerId,
    DateTime? createdAt,
    this.completedAt,
  }) : createdAt = createdAt ?? DateTime.now();

  /// 下载进度（0-100）
  double get progress {
    if (fileSize == 0) return 0;

    if (status == DownloadStatus.FAILED || status == DownloadStatus.CANCELED) {
      return (downloadedBytes / fileSize) * 100;
    }

    if (status == DownloadStatus.COMPLETED) {
      return 100;
    }

    return (downloadedBytes >= fileSize ? 1.0 : downloadedBytes / fileSize) * 100;
  }

  bool get isActive =>
      status == DownloadStatus.PENDING ||
      status == DownloadStatus.DOWNLOADING;

  String get formatSpeed {
    return '${formatCapacityString(speed)}/s';
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'file_id': fileId,
      'filename': filename,
      'file_size': fileSize,
      'file_hash': fileHash,
      'downloaded_bytes': downloadedBytes,
      'speed': speed,
      'status': status.index,
      'error_message': errorMessage,
      'save_path': savePath,
      'chunk_size': chunkSize,
      'chunk_total': chunkTotal,
      'worker_count': workerCount,
      'provider_id': providerId,
      'created_at': createdAt.toIso8601String(),
      'completed_at': completedAt?.toIso8601String(),
    };
  }

  static DownloadTaskRecord fromJson(Map<String, dynamic> json) {
    return DownloadTaskRecord(
      id: json['id'],
      fileId: json['file_id'],
      filename: json['filename'],
      fileSize: json['file_size'],
      fileHash: json['file_hash'],
      downloadedBytes: json['downloaded_bytes'] ?? 0,
      speed: json['speed'] ?? 0,
      status: json['status'] != null
          ? DownloadStatus.values[json['status']]
          : DownloadStatus.PENDING,
      errorMessage: json['error_message'],
      savePath: json['save_path'],
      chunkSize: json['chunk_size'] ?? 5 * 1024 * 1024,
      chunkTotal: json['chunk_total'] ?? 0,
      workerCount: json['worker_count'] ?? 3,
      providerId: json['provider_id'],
      createdAt: json['created_at'] != null
          ? DateTime.parse(json['created_at'])
          : DateTime.now(),
      completedAt: json['completed_at'] != null
          ? DateTime.parse(json['completed_at'])
          : null,
    );
  }

  static String createSql() {
    return '''
      CREATE TABLE IF NOT EXISTS download_tasks (
        id TEXT PRIMARY KEY,
        file_id TEXT,
        filename TEXT,
        file_size INTEGER,
        file_hash TEXT,
        downloaded_bytes INTEGER,
        speed INTEGER,
        status INTEGER,
        error_message TEXT,
        save_path TEXT,
        chunk_size INTEGER,
        chunk_total INTEGER,
        worker_count INTEGER,
        provider_id INTEGER,
        created_at TEXT,
        completed_at TEXT
      );
      CREATE INDEX idx_dt_status ON download_tasks (status);
      CREATE INDEX idx_dt_created_at ON download_tasks (created_at);
    ''';
  }
}
