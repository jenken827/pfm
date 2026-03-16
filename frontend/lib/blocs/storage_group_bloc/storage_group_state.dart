part of 'storage_group_bloc.dart';

/// 存储组状态
enum StorageGroupStatus { initial, loading, success, failure }

/// 存储组列表状态
class StorageGroupState extends Equatable {
  const StorageGroupState({
    this.status = StorageGroupStatus.initial,
    this.message = '',
    this.groups = const [],
  });

  final StorageGroupStatus status;
  final String message;
  final List<StorageGroupItem> groups;

  @override
  List<Object?> get props => [status, message, groups];

  StorageGroupState copyWith({
    StorageGroupStatus? status,
    String? message,
    List<StorageGroupItem>? groups,
  }) {
    return StorageGroupState(
      status: status ?? this.status,
      message: message ?? this.message,
      groups: groups ?? this.groups,
    );
  }
}
