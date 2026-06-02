import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/backup_bloc/backup_bloc.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/proto/native/backup.pb.dart';
import 'package:pfm/proto/native/enum.pbenum.dart';

/// 服务端备份页面
class ServerBackupPage extends StatelessWidget {
  const ServerBackupPage({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) =>
          BackupBloc(context.read<FmRepository>())..add(BackupLoadEvent()),
      child: const _ServerBackupPageView(),
    );
  }
}

class _ServerBackupPageView extends StatelessWidget {
  const _ServerBackupPageView();

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(al.serverBackup_title),
        centerTitle: true,
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => _showCreateDialog(context),
        child: const Icon(Icons.add),
      ),
      body: BlocBuilder<BackupBloc, BackupState>(
        builder: (context, state) {
          if (state.status == BackupStatus.loading) {
            return const Center(child: CircularProgressIndicator());
          }
          if (state.tasks.isEmpty) {
            return Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.backup_outlined, size: 48, color: Colors.grey[400]),
                  const SizedBox(height: 12),
                  Text(al.serverBackup_noTasks,
                      style: TextStyle(color: Colors.grey[600])),
                ],
              ),
            );
          }
          return ListView.builder(
            padding: const EdgeInsets.only(bottom: 80),
            itemCount: state.tasks.length,
            itemBuilder: (context, index) =>
                _ServerBackupTaskTile(task: state.tasks[index]),
          );
        },
      ),
    );
  }

  void _showCreateDialog(BuildContext context) {
    final bloc = context.read<BackupBloc>();
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (_) => BlocProvider.value(
        value: bloc,
        child: const _CreateServerBackupSheet(),
      ),
    );
  }
}

// ==================== Task Tile ====================

class _ServerBackupTaskTile extends StatelessWidget {
  final BackupTask task;

  const _ServerBackupTaskTile({required this.task});

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final statusText = _getStatusText(task.lastRunStatus, al);
    final statusColor = _getStatusColor(task.lastRunStatus);

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.dns_outlined,
                  size: 20,
                  color: task.hasEnabled() && task.enabled
                      ? Theme.of(context).colorScheme.primary
                      : Colors.grey,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    task.hasName() ? task.name : al.serverBackup_unnamed,
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                _StatusChip(label: statusText, color: statusColor),
              ],
            ),
            const SizedBox(height: 6),
            _InfoRow(
                icon: Icons.folder_outlined,
                text: task.hasSourcePath() ? task.sourcePath : '-'),
            if (task.hasCronExpression() && task.cronExpression.isNotEmpty)
              _InfoRow(icon: Icons.schedule, text: task.cronExpression),
            const SizedBox(height: 6),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  onPressed: () {
                    context.read<BackupBloc>().add(BackupTriggerEvent(
                        id: task.hasId() ? task.id.toInt() : 0));
                    ScaffoldMessenger.of(context).showSnackBar(
                      SnackBar(content: Text(al.serverBackup_triggered)),
                    );
                  },
                  child: Text(al.serverBackup_executeNow),
                ),
                TextButton(
                  onPressed: () => _showEditDialog(context),
                  child: Text(al.common_edit),
                ),
                TextButton(
                  onPressed: () => _confirmDelete(context),
                  style: TextButton.styleFrom(
                    foregroundColor: Theme.of(context).colorScheme.error,
                  ),
                  child: Text(al.common_delete),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  String _getStatusText(BackupRunStatus status, AppLocalizations al) {
    switch (status) {
      case BackupRunStatus.BACKUP_RUN_STATUS_IDLE:
        return al.serverBackup_statusIdle;
      case BackupRunStatus.BACKUP_RUN_STATUS_RUNNING:
        return al.serverBackup_statusRunning;
      case BackupRunStatus.BACKUP_RUN_STATUS_SUCCESS:
        return al.serverBackup_statusSuccess;
      case BackupRunStatus.BACKUP_RUN_STATUS_FAILED:
        return al.serverBackup_statusFailed;
      default:
        return al.common_unknown;
    }
  }

  Color _getStatusColor(BackupRunStatus status) {
    switch (status) {
      case BackupRunStatus.BACKUP_RUN_STATUS_RUNNING:
        return Colors.blue;
      case BackupRunStatus.BACKUP_RUN_STATUS_SUCCESS:
        return Colors.green;
      case BackupRunStatus.BACKUP_RUN_STATUS_FAILED:
        return Colors.red;
      default:
        return Colors.grey;
    }
  }

  void _showEditDialog(BuildContext context) {
    final bloc = context.read<BackupBloc>();
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (_) => BlocProvider.value(
        value: bloc,
        child: _CreateServerBackupSheet(task: task),
      ),
    );
  }

  void _confirmDelete(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final name = task.hasName() ? task.name : '';
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.serverBackup_deleteTask),
        content: Text(al.serverBackup_deleteConfirm(name)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () {
              context.read<BackupBloc>().add(BackupDeleteEvent(
                  ids: [task.hasId() ? task.id.toInt() : 0]));
              Navigator.pop(ctx);
            },
            child: Text(al.common_delete),
          ),
        ],
      ),
    );
  }
}

// ==================== 通用组件 ====================

class _StatusChip extends StatelessWidget {
  final String label;
  final Color color;

  const _StatusChip({required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withOpacity(0.1),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withOpacity(0.3)),
      ),
      child: Text(label, style: TextStyle(fontSize: 12, color: color)),
    );
  }
}

class _InfoRow extends StatelessWidget {
  final IconData icon;
  final String text;

  const _InfoRow({required this.icon, required this.text});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Row(
        children: [
          Icon(icon, size: 14, color: Colors.grey[600]),
          const SizedBox(width: 4),
          Expanded(
            child: Text(
              text,
              style: TextStyle(color: Colors.grey[700], fontSize: 12),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}

// ==================== 服务端备份创建/编辑 Sheet ====================

class _CreateServerBackupSheet extends StatefulWidget {
  final BackupTask? task;

  const _CreateServerBackupSheet({this.task});

  @override
  State<_CreateServerBackupSheet> createState() =>
      _CreateServerBackupSheetState();
}

class _CreateServerBackupSheetState extends State<_CreateServerBackupSheet> {
  final _formKey = GlobalKey<FormState>();
  late TextEditingController _nameController;
  late TextEditingController _sourcePathController;
  late TextEditingController _cronController;
  late TextEditingController _ignoreController;
  String? _targetDirId;
  String _targetDirName = '';
  BackupStrategy _strategy = BackupStrategy.BACKUP_STRATEGY_SCHEDULED;
  bool _recursive = true;
  bool _enabled = true;

  @override
  void initState() {
    super.initState();
    final task = widget.task;
    _nameController = TextEditingController(
        text: task?.hasName() == true ? task!.name : '');
    _sourcePathController = TextEditingController(
        text: task?.hasSourcePath() == true ? task!.sourcePath : '');
    _cronController = TextEditingController(
        text: task?.hasCronExpression() == true
            ? task!.cronExpression
            : '0 0 * * *');
    _ignoreController = TextEditingController(
        text: task?.hasIgnoreRules() == true ? task!.ignoreRules : '');
    if (task?.hasStrategy() == true) _strategy = task!.strategy;
    if (task?.hasRecursive() == true) _recursive = task!.recursive;
    if (task?.hasEnabled() == true) _enabled = task!.enabled;
    if (task?.hasTargetDirId() == true && task!.targetDirId.isNotEmpty) {
      _targetDirId = task.targetDirId;
      _loadTargetDirName(task.targetDirId);
    }
  }

  Future<void> _loadTargetDirName(String dirId) async {
    final result = await context.read<FmRepository>().getFileInfo(dirId);
    if (result.success && result.data != null && mounted) {
      final name = result.data!.hasName() ? result.data!.name : '';
      setState(() => _targetDirName = name);
    }
  }

  @override
  void dispose() {
    _nameController.dispose();
    _sourcePathController.dispose();
    _cronController.dispose();
    _ignoreController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context)!;
    final isEdit = widget.task != null;
    return Padding(
      padding: EdgeInsets.only(
        left: 16, right: 16, top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Form(
        key: _formKey,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(isEdit ? al.serverBackup_editTask : al.serverBackup_createTask,
                  style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 16),
              TextFormField(
                controller: _nameController,
                decoration: InputDecoration(
                    labelText: al.serverBackup_taskName, border: const OutlineInputBorder()),
                validator: (v) => v?.isEmpty == true ? al.serverBackup_taskNameRequired : null,
              ),
              const SizedBox(height: 12),
              TextFormField(
                controller: _sourcePathController,
                decoration: InputDecoration(
                  labelText: al.serverBackup_sourcePath,
                  hintText: al.serverBackup_sourcePathHint,
                  border: const OutlineInputBorder(),
                ),
                validator: (v) => v?.isEmpty == true ? al.serverBackup_sourcePathRequired : null,
              ),
              const SizedBox(height: 12),
              InkWell(
                onTap: _pickTargetFolder,
                borderRadius: BorderRadius.circular(12),
                child: InputDecorator(
                  isEmpty: _targetDirId == null,
                  decoration: InputDecoration(
                    labelText: al.serverBackup_targetDir,
                    hintText: _targetDirId == null ? al.serverBackup_targetDirHint : null,
                    border: const OutlineInputBorder(),
                    suffixIcon: const Icon(Icons.folder_open),
                  ),
                  child: _targetDirId != null
                      ? Text(_targetDirName, maxLines: 1, overflow: TextOverflow.ellipsis)
                      : null,
                ),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<BackupStrategy>(
                value: _strategy,
                decoration: InputDecoration(
                    labelText: al.serverBackup_backupStrategy, border: const OutlineInputBorder()),
                items: [
                  DropdownMenuItem(
                      value: BackupStrategy.BACKUP_STRATEGY_SCHEDULED,
                      child: Text(al.serverBackup_scheduledBackup)),
                  DropdownMenuItem(
                      value: BackupStrategy.BACKUP_STRATEGY_WATCH,
                      child: Text(al.serverBackup_watchBackup)),
                ],
                onChanged: (v) {
                  if (v != null) setState(() => _strategy = v);
                },
              ),
              if (_strategy == BackupStrategy.BACKUP_STRATEGY_SCHEDULED) ...[
                const SizedBox(height: 12),
                TextFormField(
                  controller: _cronController,
                  decoration: InputDecoration(
                    labelText: al.serverBackup_cronExpression,
                    hintText: '0 0 * * *',
                    border: const OutlineInputBorder(),
                  ),
                ),
              ],
              const SizedBox(height: 12),
              TextFormField(
                controller: _ignoreController,
                decoration: InputDecoration(
                  labelText: al.serverBackup_ignoreRules,
                  hintText: al.serverBackup_ignoreRulesHint,
                  border: const OutlineInputBorder(),
                ),
                maxLines: 2,
              ),
              SwitchListTile(
                  title: Text(al.serverBackup_includeSubdir),
                  value: _recursive,
                  onChanged: (v) => setState(() => _recursive = v)),
              SwitchListTile(
                  title: Text(al.clientBackup_enable),
                  value: _enabled,
                  onChanged: (v) => setState(() => _enabled = v)),
              const SizedBox(height: 16),
              FilledButton(
                  onPressed: _submit, child: Text(isEdit ? al.common_save : al.common_create)),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _pickTargetFolder() async {
    final result = await Navigator.of(context).push<FolderPickResult>(
      MaterialPageRoute(builder: (context) => const FolderPickerPage()),
    );
    if (result != null && mounted) {
      setState(() {
        _targetDirId = result.id;
        _targetDirName = result.name;
      });
    }
  }

  void _submit() {
    final al = AppLocalizations.of(context)!;
    if (!_formKey.currentState!.validate()) return;
    if (_targetDirId == null) {
      ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.serverBackup_selectTargetDir)));
      return;
    }
    final bloc = context.read<BackupBloc>();
    if (widget.task != null) {
      bloc.add(BackupUpdateEvent(
        id: widget.task!.hasId() ? widget.task!.id.toInt() : 0,
        name: _nameController.text,
        sourcePath: _sourcePathController.text,
        targetDirId: _targetDirId!,
        strategy: _strategy,
        cronExpression: _cronController.text,
        ignoreRules:
            _ignoreController.text.isEmpty ? null : _ignoreController.text,
        recursive: _recursive,
        enabled: _enabled,
      ));
    } else {
      bloc.add(BackupCreateEvent(
        name: _nameController.text,
        sourcePath: _sourcePathController.text,
        targetDirId: _targetDirId!,
        strategy: _strategy,
        cronExpression: _cronController.text,
        ignoreRules:
            _ignoreController.text.isEmpty ? null : _ignoreController.text,
        recursive: _recursive,
        enabled: _enabled,
      ));
    }
    Navigator.pop(context);
  }
}
