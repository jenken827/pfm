part of 'transfer_bloc.dart';

/// 传输事件基类
class TransferEvent {
  const TransferEvent();
}

// 订阅事件
class TransferUploadSubscribeEvent extends TransferEvent {}

class TransferDownloadSubscribeEvent extends TransferEvent {}

/// 刷新传输列表
class TransferRefreshEvent extends TransferEvent {}

/// 清除已完成的传输任务
class TransferClearCompletedEvent extends TransferEvent {}

/// 清除失败的传输任务
class TransferClearFailedEvent extends TransferEvent {}

// 新增上传任务
class TransferAddUploadEvent extends TransferEvent {
  TransferAddUploadEvent(
    this.filePath,
    this.parentId, {
    this.tags,
    this.overwrite = false,
  });

  final String filePath;
  final String parentId;
  final String? tags;
  final bool overwrite;
}

/// 暂停上传，如果不传taskId，则暂停所有
class TransferPauseEvent extends TransferEvent {
  TransferPauseEvent(this.taskId);
  final String? taskId;
}

/// 恢复
class TransferResumeEvent extends TransferEvent {
  TransferResumeEvent(this.taskId);
  final String? taskId;
}

/// 删除
class TransferRemoveEvent extends TransferEvent {
  TransferRemoveEvent(this.taskId);
  final String? taskId;
}

/// 取消
class TransferCancelEvent extends TransferEvent {
  TransferCancelEvent(this.taskId);
  final String? taskId;
}
