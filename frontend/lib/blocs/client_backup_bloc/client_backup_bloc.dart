import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';

part 'client_backup_event.dart';
part 'client_backup_state.dart';

class ClientBackupBloc extends Bloc<ClientBackupEvent, ClientBackupState> {
  ClientBackupBloc() : super(const ClientBackupState()) {
    on<ClientBackupLoadEvent>(_onLoad);
    on<ClientBackupCreateEvent>(_onCreate);
    on<ClientBackupUpdateEvent>(_onUpdate);
    on<ClientBackupDeleteEvent>(_onDelete);
    on<ClientBackupTriggerEvent>(_onTrigger);
  }

  Future<void> _onLoad(
    ClientBackupLoadEvent event,
    Emitter<ClientBackupState> emit,
  ) async {
    emit(state.copyWith(status: ClientBackupStatus.loading));
    try {
      final configs = ClientBackupService.instance.getConfigs();
      emit(
        state.copyWith(status: ClientBackupStatus.success, configs: configs),
      );
    } catch (e) {
      emit(
        state.copyWith(
          status: ClientBackupStatus.failure,
          message: e.toString(),
        ),
      );
    }
  }

  Future<void> _onCreate(
    ClientBackupCreateEvent event,
    Emitter<ClientBackupState> emit,
  ) async {
    try {
      await ClientBackupService.instance.addConfig(
        ClientBackupConfig(
          id: event.id,
          name: event.name,
          sourcePath: event.sourcePath,
          targetDirId: event.targetDirId,
          strategy: event.strategy,
          cronExpression: event.cronExpression,
          ignoreRules: event.ignoreRules,
          recursive: event.recursive,
          enabled: event.enabled,
        ),
      );
      add(ClientBackupLoadEvent());
    } catch (e) {
      emit(
        state.copyWith(
          status: ClientBackupStatus.failure,
          message: e.toString(),
        ),
      );
    }
  }

  Future<void> _onUpdate(
    ClientBackupUpdateEvent event,
    Emitter<ClientBackupState> emit,
  ) async {
    try {
      final configs = ClientBackupService.instance.getConfigs();
      final config = configs.firstWhere((c) => c.id == event.id);
      final updated = config.copyWith(
        name: event.name,
        sourcePath: event.sourcePath,
        targetDirId: event.targetDirId,
        strategy: event.strategy,
        cronExpression: event.cronExpression,
        ignoreRules: event.ignoreRules,
        recursive: event.recursive,
        enabled: event.enabled,
      );
      await ClientBackupService.instance.updateConfig(updated);
      add(ClientBackupLoadEvent());
    } catch (e) {
      emit(
        state.copyWith(
          status: ClientBackupStatus.failure,
          message: e.toString(),
        ),
      );
    }
  }

  Future<void> _onDelete(
    ClientBackupDeleteEvent event,
    Emitter<ClientBackupState> emit,
  ) async {
    try {
      for (final id in event.ids) {
        await ClientBackupService.instance.deleteConfig(id);
      }
      add(ClientBackupLoadEvent());
    } catch (e) {
      emit(
        state.copyWith(
          status: ClientBackupStatus.failure,
          message: e.toString(),
        ),
      );
    }
  }

  Future<void> _onTrigger(
    ClientBackupTriggerEvent event,
    Emitter<ClientBackupState> emit,
  ) async {
    emit(state.copyWith(runningConfigId: event.id));
    try {
      final result = await ClientBackupService.instance.executeBackup(event.id);
      add(ClientBackupLoadEvent());
      emit(state.copyWith(lastResult: result, runningConfigId: ''));
    } catch (e) {
      emit(
        state.copyWith(
          status: ClientBackupStatus.failure,
          message: e.toString(),
          runningConfigId: '',
        ),
      );
    }
  }
}
