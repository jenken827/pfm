part of 'client_backup_bloc.dart';

enum ClientBackupStatus { initial, loading, success, failure }

class ClientBackupState extends Equatable {
  final ClientBackupStatus status;
  final List<ClientBackupConfig> configs;
  final String? message;
  final String runningConfigId;
  final (int, int, int)? lastResult; // (scanned, toBackup, created)

  const ClientBackupState({
    this.status = ClientBackupStatus.initial,
    this.configs = const [],
    this.message,
    this.runningConfigId = '',
    this.lastResult,
  });

  ClientBackupState copyWith({
    ClientBackupStatus? status,
    List<ClientBackupConfig>? configs,
    String? message,
    String? runningConfigId,
    (int, int, int)? lastResult,
  }) {
    return ClientBackupState(
      status: status ?? this.status,
      configs: configs ?? this.configs,
      message: message ?? this.message,
      runningConfigId: runningConfigId ?? this.runningConfigId,
      lastResult: lastResult ?? this.lastResult,
    );
  }

  @override
  List<Object?> get props => [
    status,
    configs,
    message,
    runningConfigId,
    lastResult,
  ];
}
