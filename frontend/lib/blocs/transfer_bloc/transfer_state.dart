part of 'transfer_bloc.dart';

/// 传输标签页
enum TransferTab { upload, download }

/// 传输状态
class TransferState extends Equatable {
  final TransferTab currentTab;
  final List<UploadTaskRecord> uploadTasks;
  final List<DownloadTaskState> downloadTasks;

  const TransferState({
    this.currentTab = TransferTab.upload,
    this.uploadTasks = const [],
    this.downloadTasks = const [],
  });

  bool get isUpload => currentTab == TransferTab.upload;

  /// 正在上传的任务数
  int get uploadingCount => uploadTasks
      .where(
        (t) =>
            t.status == UploadStatus.UPLOAD_STATUS_UPLOADING ||
            t.status == UploadStatus.UPLOAD_STATUS_PENDING,
      )
      .length;

  /// 正在下载的任务数
  int get downloadingCount => downloadTasks
      .where((t) => t.status == DownloadTaskStatus.downloading)
      .length;

  /// 已完成的上传任务数
  int get completedUploadCount => uploadTasks
      .where((t) => t.status == UploadStatus.UPLOAD_STATUS_COMPLETED)
      .length;

  /// 失败的上传任务数
  int get failedUploadCount => uploadTasks
      .where((t) => t.status == UploadStatus.UPLOAD_STATUS_FAILED)
      .length;

  /// 是否有活动的传输任务
  bool get hasActiveTransfers => uploadingCount > 0 || downloadingCount > 0;

  /// 活动任务总数
  int get activeTransferCount => uploadingCount + downloadingCount;

  TransferState copyWith({
    TransferTab? currentTab,
    List<UploadTaskRecord>? uploadTasks,
    List<DownloadTaskState>? downloadTasks,
  }) {
    return TransferState(
      currentTab: currentTab ?? this.currentTab,
      uploadTasks: uploadTasks ?? this.uploadTasks,
      downloadTasks: downloadTasks ?? this.downloadTasks,
    );
  }

  @override
  List<Object?> get props => [currentTab, uploadTasks, downloadTasks];
}

/// 下载任务状态
enum DownloadTaskStatus {
  pending,
  downloading,
  paused,
  completed,
  failed,
  cancelled,
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

  const DownloadTaskState({
    required this.taskId,
    required this.fileName,
    required this.fileId,
    required this.fileSize,
    this.downloadedSize = 0,
    required this.status,
    this.errorMessage,
    this.localPath,
  });

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
  ];
}
