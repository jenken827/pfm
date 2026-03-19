import 'package:equatable/equatable.dart';

/// 上传事件基类
abstract class UploadEvent extends Equatable {
  const UploadEvent();

  @override
  List<Object?> get props => [];
}

/// 开始上传文件
class UploadStartEvent extends UploadEvent {
  final String filePath;
  final String? parentId;

  const UploadStartEvent({
    required this.filePath,
    this.parentId,
  });

  @override
  List<Object?> get props => [filePath, parentId];
}

/// 暂停上传
class UploadPauseEvent extends UploadEvent {
  final String taskId;

  const UploadPauseEvent(this.taskId);

  @override
  List<Object?> get props => [taskId];
}

/// 恢复上传
class UploadResumeEvent extends UploadEvent {
  final String taskId;

  const UploadResumeEvent(this.taskId);

  @override
  List<Object?> get props => [taskId];
}

/// 取消上传
class UploadCancelEvent extends UploadEvent {
  final String taskId;

  const UploadCancelEvent(this.taskId);

  @override
  List<Object?> get props => [taskId];
}

/// 重试上传
class UploadRetryEvent extends UploadEvent {
  final String taskId;

  const UploadRetryEvent(this.taskId);

  @override
  List<Object?> get props => [taskId];
}

/// 移除上传任务
class UploadRemoveEvent extends UploadEvent {
  final String taskId;

  const UploadRemoveEvent(this.taskId);

  @override
  List<Object?> get props => [taskId];
}

/// 清除已完成的上传任务
class UploadClearCompletedEvent extends UploadEvent {
  const UploadClearCompletedEvent();
}
