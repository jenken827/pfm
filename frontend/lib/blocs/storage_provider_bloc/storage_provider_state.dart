part of 'storage_provider_bloc.dart';

/// 存储介质状态
enum StorageProviderStatus { initial, loading, success, failure }

/// 存储介质列表状态
class StorageProviderState extends Equatable {
  const StorageProviderState({
    this.status = StorageProviderStatus.initial,
    this.message = '',
    this.providers = const [],
  });

  final StorageProviderStatus status;
  final String message;
  final List<StorageProvider> providers;

  @override
  List<Object?> get props => [status, message, providers];

  StorageProviderState copyWith({
    StorageProviderStatus? status,
    String? message,
    List<StorageProvider>? providers,
  }) {
    return StorageProviderState(
      status: status ?? this.status,
      message: message ?? this.message,
      providers: providers ?? this.providers,
    );
  }
}
