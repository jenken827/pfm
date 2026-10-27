part of 'storage_provider_bloc.dart';

/// 存储介质事件基类
sealed class StorageProviderEvent {
  const StorageProviderEvent();
}

/// 加载存储提供者类型列表
class StorageProviderLoadTypesEvent extends StorageProviderEvent {
  const StorageProviderLoadTypesEvent();
}

/// 加载存储介质列表
class StorageProviderLoadEvent extends StorageProviderEvent {
  const StorageProviderLoadEvent();
}

/// 写回补拉到的类型列表（页面在首次拉取失败后自行补拉时使用）
class StorageProviderTypesLoadedEvent extends StorageProviderEvent {
  const StorageProviderTypesLoadedEvent(this.types);

  final List<DriverInfo> types;
}

/// 添加存储介质
class StorageProviderAddEvent extends StorageProviderEvent {
  const StorageProviderAddEvent({
    required this.driverId,
    required this.name,
    this.description,
    this.config,
    this.enabled,
    this.sortOrder,
    this.capacity,
    this.fileSizeLimit,
    this.encryptionKey,
    this.encryptionSuffix,
    this.defaultBound,
    this.proxy,
    this.maxConcurrentUploads,
  });

  final String driverId;
  final String name;
  final String? description;
  final String? config;
  final bool? enabled;
  final int? sortOrder;
  final int? capacity;
  final int? fileSizeLimit;
  final String? encryptionKey;
  final String? encryptionSuffix;
  final bool? defaultBound;
  final String? proxy;
  final int? maxConcurrentUploads;
}

/// 更新存储介质
class StorageProviderUpdateEvent extends StorageProviderEvent {
  const StorageProviderUpdateEvent({
    required this.id,
    this.name,
    this.description,
    this.config,
    this.enabled,
    this.sortOrder,
    this.capacity,
    this.fileSizeLimit,
    this.defaultBound,
    this.proxy,
    this.maxConcurrentUploads,
  });

  final int id;
  final String? name;
  final String? description;
  final String? config;
  final bool? enabled;
  final int? sortOrder;
  final int? capacity;
  final int? fileSizeLimit;
  final bool? defaultBound;
  final String? proxy;
  final int? maxConcurrentUploads;
}

/// 删除存储介质
class StorageProviderDeleteEvent extends StorageProviderEvent {
  const StorageProviderDeleteEvent(this.id);

  final int id;
}

/// 切换存储介质启用状态
class StorageProviderToggleEvent extends StorageProviderEvent {
  const StorageProviderToggleEvent({required this.id, required this.enabled});

  final int id;
  final bool enabled;
}
