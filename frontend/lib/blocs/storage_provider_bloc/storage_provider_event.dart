part of 'storage_provider_bloc.dart';

/// 存储介质事件基类
sealed class StorageProviderEvent {
  const StorageProviderEvent();
}

/// 加载存储介质列表
class StorageProviderLoadEvent extends StorageProviderEvent {
  const StorageProviderLoadEvent();
}

/// 加载存储组列表
class StorageProviderGroupsEvent extends StorageProviderEvent {
  const StorageProviderGroupsEvent();
}

/// 添加存储介质
class StorageProviderAddEvent extends StorageProviderEvent {
  const StorageProviderAddEvent({
    required this.providerType,
    required this.name,
    this.description,
    this.config,
    required this.groupId,
    this.enabled = true,
    this.order = 0,
    required this.capacity,
    required this.fileSizeLimit,
  });

  final StorageProviderType providerType;
  final String name;
  final String? description;
  final String? config;
  final int groupId;
  final bool enabled;
  final int order;
  final int capacity;
  final int fileSizeLimit;
}

/// 更新存储介质
class StorageProviderUpdateEvent extends StorageProviderEvent {
  const StorageProviderUpdateEvent({
    required this.id,
    this.name,
    this.description,
    this.config,
    this.groupId,
    this.enabled,
    this.order,
    this.capacity,
    this.fileSizeLimit,
  });

  final int id;
  final String? name;
  final String? description;
  final String? config;
  final int? groupId;
  final bool? enabled;
  final int? order;
  final int? capacity;
  final int? fileSizeLimit;
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
