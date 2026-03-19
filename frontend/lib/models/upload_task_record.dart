/// 上传任务状态
enum UploadTaskStatusRecord {
  pending,
  initializing,
  uploading,
  paused,
  completing,
  completed,
  failed,
  cancelled,
}

/// 上传任务持久化记录
///
/// 纯 Dart 模型类，支持 SQLite 存储
class UploadTaskRecord {
  String id;
  String filePath;
  String fileName;
  int fileSize;
  String? parentId;
  String? fileHash;
  String? mime;
  int? fileType;
  int chunkSize;
  int chunkTotal;
  int chunkIndex;
  List<int> uploadedChunks;
  UploadTaskStatusRecord status;
  String? errorMessage;
  String? uploadId;
  String? fileId;
  DateTime createdAt;
  DateTime? completedAt;
  int retryCount;
  int uploadedBytes;
  double hashProgress; // 哈希计算进度 (0-100)

  UploadTaskRecord({
    required this.id,
    required this.filePath,
    required this.fileName,
    required this.fileSize,
    this.parentId,
    this.fileHash,
    this.mime,
    this.fileType,
    required this.chunkSize,
    required this.chunkTotal,
    this.chunkIndex = 0,
    List<int>? uploadedChunks,
    this.status = UploadTaskStatusRecord.pending,
    this.errorMessage,
    this.uploadId,
    this.fileId,
    DateTime? createdAt,
    this.completedAt,
    this.retryCount = 0,
    this.uploadedBytes = 0,
    this.hashProgress = 0,
  })  : uploadedChunks = uploadedChunks ?? [],
        createdAt = createdAt ?? DateTime.now();

  /// 上传进度（0-100）
  double get progress {
    if (chunkTotal == 0) return 0;
    return (uploadedChunks.length / chunkTotal) * 100;
  }

  /// 是否正在上传
  bool get isUploading => status == UploadTaskStatusRecord.uploading;

  /// 是否正在初始化（计算 Hash）
  bool get isInitializing => status == UploadTaskStatusRecord.initializing;

  /// 是否已完成
  bool get isCompleted => status == UploadTaskStatusRecord.completed;

  /// 是否失败
  bool get isFailed => status == UploadTaskStatusRecord.failed;

  /// 是否暂停
  bool get isPaused => status == UploadTaskStatusRecord.paused;

  /// 是否取消
  bool get isCancelled => status == UploadTaskStatusRecord.cancelled;

  /// 是否可以重试
  bool get canRetry => isFailed && retryCount < 3;

  /// 复制并更新
  UploadTaskRecord copyWith({
    String? id,
    String? filePath,
    String? fileName,
    int? fileSize,
    String? parentId,
    String? fileHash,
    String? mime,
    int? fileType,
    int? chunkSize,
    int? chunkTotal,
    int? chunkIndex,
    List<int>? uploadedChunks,
    UploadTaskStatusRecord? status,
    String? errorMessage,
    String? uploadId,
    String? fileId,
    DateTime? createdAt,
    DateTime? completedAt,
    int? retryCount,
    int? uploadedBytes,
    double? hashProgress,
  }) {
    return UploadTaskRecord(
      id: id ?? this.id,
      filePath: filePath ?? this.filePath,
      fileName: fileName ?? this.fileName,
      fileSize: fileSize ?? this.fileSize,
      parentId: parentId ?? this.parentId,
      fileHash: fileHash ?? this.fileHash,
      mime: mime ?? this.mime,
      fileType: fileType ?? this.fileType,
      chunkSize: chunkSize ?? this.chunkSize,
      chunkTotal: chunkTotal ?? this.chunkTotal,
      chunkIndex: chunkIndex ?? this.chunkIndex,
      uploadedChunks: uploadedChunks ?? List.from(this.uploadedChunks),
      status: status ?? this.status,
      errorMessage: errorMessage ?? this.errorMessage,
      uploadId: uploadId ?? this.uploadId,
      fileId: fileId ?? this.fileId,
      createdAt: createdAt ?? this.createdAt,
      completedAt: completedAt ?? this.completedAt,
      retryCount: retryCount ?? this.retryCount,
      uploadedBytes: uploadedBytes ?? this.uploadedBytes,
      hashProgress: hashProgress ?? this.hashProgress,
    );
  }

  @override
  String toString() {
    return 'UploadTaskRecord(id: $id, fileName: $fileName, status: $status, progress: ${progress.toStringAsFixed(1)}%)';
  }
}
