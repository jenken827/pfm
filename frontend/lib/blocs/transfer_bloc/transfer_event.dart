import 'package:equatable/equatable.dart';

/// 传输事件基类
abstract class TransferEvent extends Equatable {
  const TransferEvent();

  @override
  List<Object?> get props => [];
}

/// 刷新传输列表
class TransferRefreshEvent extends TransferEvent {}

/// 清除已完成的传输任务
class TransferClearCompletedEvent extends TransferEvent {}

/// 清除失败的传输任务
class TransferClearFailedEvent extends TransferEvent {}

/// 暂停所有上传
class TransferPauseAllUploadsEvent extends TransferEvent {}

/// 恢复所有上传
class TransferResumeAllUploadsEvent extends TransferEvent {}

/// 取消所有上传
class TransferCancelAllUploadsEvent extends TransferEvent {}
