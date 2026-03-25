part of 'storage_provider_bloc.dart';

/// 存储介质状态
enum StorageProviderStatus { initial, loading, success, failure }

/// 存储介质列表状态
class StorageProviderState extends Equatable {
  const StorageProviderState({
    this.status = StorageProviderStatus.initial,
    this.message = '',
    this.providers = const [],
    this.groups = const [],
  });

  final StorageProviderStatus status;
  final String message;
  final List<StorageProviderItem> providers;
  final List<StorageGroupItem> groups;

  @override
  List<Object?> get props => [status, message, providers, groups];

  StorageProviderState copyWith({
    StorageProviderStatus? status,
    String? message,
    List<StorageProviderItem>? providers,
    List<StorageGroupItem>? groups,
  }) {
    return StorageProviderState(
      status: status ?? this.status,
      message: message ?? this.message,
      providers: providers ?? this.providers,
      groups: groups ?? this.groups,
    );
  }
}
