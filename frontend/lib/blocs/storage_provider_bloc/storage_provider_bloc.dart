import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';
import 'package:pfm/proto/native/provider.pb.dart';

part 'storage_provider_event.dart';
part 'storage_provider_state.dart';

/// 存储介质 Bloc
class StorageProviderBloc
    extends Bloc<StorageProviderEvent, StorageProviderState> {
  final FmRepository _repository;

  StorageProviderBloc(this._repository) : super(const StorageProviderState()) {
    on<StorageProviderLoadTypesEvent>(_onLoadTypes);
    on<StorageProviderLoadEvent>(_onLoad);
    on<StorageProviderAddEvent>(_onAdd);
    on<StorageProviderUpdateEvent>(_onUpdate);
    on<StorageProviderDeleteEvent>(_onDelete);
    on<StorageProviderToggleEvent>(_onToggle);
  }

  Future<void> _onLoadTypes(
    StorageProviderLoadTypesEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    emit(state.copyWith(status: StorageProviderStatus.loading));

    final result = await _repository.getStorageProviderTypes();

    if (result.success) {
      emit(
        state.copyWith(
          status: StorageProviderStatus.success,
          providerTypes: result.data?.types.toList() ?? [],
        ),
      );
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '获取存储提供者类型失败',
        ),
      );
    }
  }

  Future<void> _onLoad(
    StorageProviderLoadEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    emit(state.copyWith(status: StorageProviderStatus.loading));

    final result = await _repository.listStorageProviders();

    if (result.success) {
      emit(
        state.copyWith(
          status: StorageProviderStatus.success,
          providers: result.data?.providers.toList() ?? [],
        ),
      );
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '获取失败',
        ),
      );
    }
  }

  Future<void> _onAdd(
    StorageProviderAddEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    final result = await _repository.addStorageProvider(
      providerType: event.providerType,
      name: event.name,
      description: event.description,
      config: event.config,
      enabled: event.enabled,
      order: event.order,
      totalCapacity: event.capacity,
      fileSizeLimit: event.fileSizeLimit,
      encryptionKey: event.encryptionKey,
      encryptionSuffix: event.encryptionSuffix,
      defaultBound: event.defaultBound,
      proxy: event.proxy,
    );

    if (result.success) {
      add(const StorageProviderLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '添加失败',
        ),
      );
    }
  }

  Future<void> _onUpdate(
    StorageProviderUpdateEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    final result = await _repository.updateStorageProvider(
      id: event.id,
      name: event.name,
      description: event.description,
      config: event.config,
      enabled: event.enabled,
      order: event.order,
      totalCapacity: event.capacity,
      fileSizeLimit: event.fileSizeLimit,
      defaultBound: event.defaultBound,
      proxy: event.proxy,
    );

    if (result.success) {
      add(const StorageProviderLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '更新失败',
        ),
      );
    }
  }

  Future<void> _onDelete(
    StorageProviderDeleteEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    final result = await _repository.deleteStorageProvider([event.id]);

    if (result.success) {
      add(const StorageProviderLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '删除失败',
        ),
      );
    }
  }

  Future<void> _onToggle(
    StorageProviderToggleEvent event,
    Emitter<StorageProviderState> emit,
  ) async {
    final result = await _repository.updateStorageProvider(
      id: event.id,
      enabled: event.enabled,
    );

    if (result.success) {
      add(const StorageProviderLoadEvent());
    } else {
      emit(
        state.copyWith(
          status: StorageProviderStatus.failure,
          message: result.message ?? '更新失败',
        ),
      );
    }
  }
}
