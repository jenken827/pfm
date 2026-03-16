import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/group.pb.dart';

part 'storage_group_event.dart';
part 'storage_group_state.dart';

/// 存储组 Bloc
class StorageGroupBloc extends Bloc<StorageGroupEvent, StorageGroupState> {
  final FmRepository _repository;

  StorageGroupBloc(this._repository) : super(const StorageGroupState()) {
    on<StorageGroupLoadEvent>(_onLoad);
    on<StorageGroupAddEvent>(_onAdd);
    on<StorageGroupUpdateEvent>(_onUpdate);
    on<StorageGroupDeleteEvent>(_onDelete);
  }

  Future<void> _onLoad(
    StorageGroupLoadEvent event,
    Emitter<StorageGroupState> emit,
  ) async {
    emit(state.copyWith(status: StorageGroupStatus.loading));

    final result = await _repository.listStorageGroups(withStats: true);

    if (result.success) {
      emit(state.copyWith(
        status: StorageGroupStatus.success,
        groups: result.data?.groups.toList() ?? [],
      ));
    } else {
      emit(state.copyWith(
        status: StorageGroupStatus.failure,
        message: result.message ?? '获取失败',
      ));
    }
  }

  Future<void> _onAdd(
    StorageGroupAddEvent event,
    Emitter<StorageGroupState> emit,
  ) async {
    final result = await _repository.addStorageGroup(
      name: event.name,
      description: event.description,
    );

    if (result.success) {
      add(const StorageGroupLoadEvent());
    } else {
      emit(state.copyWith(
        status: StorageGroupStatus.failure,
        message: result.message ?? '添加失败',
      ));
    }
  }

  Future<void> _onUpdate(
    StorageGroupUpdateEvent event,
    Emitter<StorageGroupState> emit,
  ) async {
    final result = await _repository.updateStorageGroup(
      id: event.id,
      name: event.name,
      description: event.description,
    );

    if (result.success) {
      add(const StorageGroupLoadEvent());
    } else {
      emit(state.copyWith(
        status: StorageGroupStatus.failure,
        message: result.message ?? '更新失败',
      ));
    }
  }

  Future<void> _onDelete(
    StorageGroupDeleteEvent event,
    Emitter<StorageGroupState> emit,
  ) async {
    final result = await _repository.deleteStorageGroup(event.id);

    if (result.success) {
      add(const StorageGroupLoadEvent());
    } else {
      emit(state.copyWith(
        status: StorageGroupStatus.failure,
        message: result.message ?? '删除失败',
      ));
    }
  }
}
