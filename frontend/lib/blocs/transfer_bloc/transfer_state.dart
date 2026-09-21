part of 'transfer_bloc.dart';

/// 传输标签页
enum TransferTab { upload, download, uploadRecord }

/// 传输状态
class TransferState extends Equatable {
  final TransferTab currentTab;
  final List<UploadTaskRecord> uploadTasks;
  final List<DownloadTaskState> downloadTasks;
  final int fakeStatus;
  final int completedUploadCountSnapshot;
  // 从数据库获取的真实计数（不受 100 条限制）
  final int dbUploadingCount;
  final int dbPendingCount;
  final int dbCompletedCount;
  final int dbFailedCount;
  final int dbDownloadingCount;

  const TransferState({
    this.currentTab = TransferTab.upload,
    this.uploadTasks = const [],
    this.downloadTasks = const [],
    this.fakeStatus = 0,
    this.completedUploadCountSnapshot = 0,
    this.dbUploadingCount = 0,
    this.dbPendingCount = 0,
    this.dbCompletedCount = 0,
    this.dbFailedCount = 0,
    this.dbDownloadingCount = 0,
  });

  bool get isUpload => currentTab == TransferTab.upload;

  /// 正在上传的任务数
  int get uploadingCount => uploadTasks.where((t) => t.isActive).length;

  /// 正在下载的任务数
  int get downloadingCount => downloadTasks
      .where((t) => t.status == DownloadTaskStatus.downloading)
      .length;

  /// 已完成的上传任务数
  int get completedUploadCount =>
      uploadTasks.where((t) => t.status == UploadStatus.COMPLETED).length;

  /// 失败的上传任务数
  int get failedUploadCount =>
      uploadTasks.where((t) => t.status == UploadStatus.FAILED).length;

  /// 等待中的上传任务数
  int get pendingUploadCount =>
      uploadTasks.where((t) => t.status == UploadStatus.PENDING).length;

  /// 已暂停的上传任务数
  int get pausedUploadCount =>
      uploadTasks.where((t) => t.status == UploadStatus.PAUSED).length;

  /// 是否有活动的传输任务
  bool get hasActiveTransfers => uploadingCount > 0 || downloadingCount > 0;

  /// 检测是否有新的上传任务完成
  bool hasNewCompletedUploads(TransferState previous) {
    final currentCompleted = uploadTasks
        .where((t) => t.status == UploadStatus.COMPLETED)
        .length;
    return currentCompleted > previous.completedUploadCountSnapshot;
  }

  /// 活动任务总数
  int get activeTransferCount => uploadingCount + downloadingCount;

  TransferState copyWith({
    TransferTab? currentTab,
    List<UploadTaskRecord>? uploadTasks,
    List<DownloadTaskState>? downloadTasks,
    int? fakeStatus,
    int? completedUploadCountSnapshot,
    int? dbUploadingCount,
    int? dbPendingCount,
    int? dbCompletedCount,
    int? dbFailedCount,
    int? dbDownloadingCount,
  }) {
    return TransferState(
      currentTab: currentTab ?? this.currentTab,
      uploadTasks: uploadTasks ?? this.uploadTasks,
      downloadTasks: downloadTasks ?? this.downloadTasks,
      fakeStatus: fakeStatus ?? this.fakeStatus,
      completedUploadCountSnapshot:
          completedUploadCountSnapshot ?? this.completedUploadCountSnapshot,
      dbUploadingCount: dbUploadingCount ?? this.dbUploadingCount,
      dbPendingCount: dbPendingCount ?? this.dbPendingCount,
      dbCompletedCount: dbCompletedCount ?? this.dbCompletedCount,
      dbFailedCount: dbFailedCount ?? this.dbFailedCount,
      dbDownloadingCount: dbDownloadingCount ?? this.dbDownloadingCount,
    );
  }

  @override
  List<Object?> get props => [
    currentTab,
    uploadTasks,
    downloadTasks,
    fakeStatus,
    completedUploadCountSnapshot,
    dbUploadingCount,
    dbPendingCount,
    dbCompletedCount,
    dbFailedCount,
    dbDownloadingCount,
  ];
}

/// 下载任务状态
enum DownloadTaskStatus {
  pending,
  downloading,
  paused,
  completed,
  failed,
  cancelled,
  preDownload, // 预下载中（Telegram 大文件等待缓存）
}

/// 下载任务
class DownloadTaskState extends Equatable {
  final String taskId;
  final String fileName;
  final String fileId;
  final int fileSize;
  final int downloadedSize;
  final DownloadTaskStatus status;
  final String? errorMessage;
  final String? localPath;
  /// 已发布到公共「下载」目录时的 MediaStore content:// uri。
  /// Android 上打开文件必须用它，而不是 localPath（详见 DownloadDir.openPublished 注释）。
  final String? contentUri;

  const DownloadTaskState({
    required this.taskId,
    required this.fileName,
    required this.fileId,
    required this.fileSize,
    this.downloadedSize = 0,
    required this.status,
    this.errorMessage,
    this.localPath,
    this.contentUri,
  });

  /// 是否预下载中
  bool get isPreDownload => status == DownloadTaskStatus.preDownload;

  /// 下载进度（0-100）
  double get progress {
    if (fileSize == 0) return 0;
    return (downloadedSize / fileSize) * 100;
  }

  /// 是否下载中
  bool get isDownloading => status == DownloadTaskStatus.downloading;

  /// 是否已完成
  bool get isCompleted => status == DownloadTaskStatus.completed;

  /// 是否失败
  bool get isFailed => status == DownloadTaskStatus.failed;

  /// 是否暂停
  bool get isPaused => status == DownloadTaskStatus.paused;

  DownloadTaskState copyWith({
    int? downloadedSize,
    DownloadTaskStatus? status,
    String? errorMessage,
    String? localPath,
    String? contentUri,
  }) {
    return DownloadTaskState(
      taskId: taskId,
      fileName: fileName,
      fileId: fileId,
      fileSize: fileSize,
      downloadedSize: downloadedSize ?? this.downloadedSize,
      status: status ?? this.status,
      errorMessage: errorMessage ?? this.errorMessage,
      localPath: localPath ?? this.localPath,
      contentUri: contentUri ?? this.contentUri,
    );
  }

  @override
  List<Object?> get props => [
    taskId,
    fileName,
    fileId,
    fileSize,
    downloadedSize,
    status,
    errorMessage,
    localPath,
    contentUri,
  ];
}
