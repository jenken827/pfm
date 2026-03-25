part of 'storage_group_bloc.dart';

/// 存储组事件基类
sealed class StorageGroupEvent {
  const StorageGroupEvent();
}

/// 加载存储组列表
class StorageGroupLoadEvent extends StorageGroupEvent {
  const StorageGroupLoadEvent();
}

/// 添加存储组
class StorageGroupAddEvent extends StorageGroupEvent {
  const StorageGroupAddEvent({
    required this.name,
    this.description,
    this.order = 0,
  });

  final String name;
  final String? description;
  final int order;
}

/// 更新存储组
class StorageGroupUpdateEvent extends StorageGroupEvent {
  const StorageGroupUpdateEvent({
    required this.id,
    this.name,
    this.description,
    this.order,
  });

  final int id;
  final String? name;
  final String? description;
  final int? order;
}

/// 批量更新存储组顺序
class StorageGroupReorderEvent extends StorageGroupEvent {
  const StorageGroupReorderEvent(this.groups);

  final List<StorageGroupItem> groups;
}

/// 删除存储组
class StorageGroupDeleteEvent extends StorageGroupEvent {
  const StorageGroupDeleteEvent(this.id);

  final int id;
}
