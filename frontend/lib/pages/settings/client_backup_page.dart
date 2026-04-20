import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/client_backup_bloc/client_backup_bloc.dart';
import 'package:pfm/models/client_backup_config.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/utils/platform.dart';
import 'package:uuid/uuid.dart';

/// 客户端备份页面
class ClientBackupPage extends StatelessWidget {
  const ClientBackupPage({super.key});

  @override
  Widget build(BuildContext context) {
    return BlocProvider(
      create: (context) =>
          ClientBackupBloc()..add(ClientBackupLoadEvent()),
      child: const _ClientBackupPageView(),
    );
  }
}

class _ClientBackupPageView extends StatelessWidget {
  const _ClientBackupPageView();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('自动备份'),
        centerTitle: true,
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => _showCreateDialog(context),
        child: const Icon(Icons.add),
      ),
      body: BlocBuilder<ClientBackupBloc, ClientBackupState>(
        builder: (context, state) {
          if (state.configs.isEmpty) {
            return Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.phone_android_outlined, size: 48, color: Colors.grey[400]),
                  const SizedBox(height: 12),
                  Text('暂无自动备份任务',
                      style: TextStyle(color: Colors.grey[600])),
                ],
              ),
            );
          }
          return ListView.builder(
            padding: const EdgeInsets.only(bottom: 80),
            itemCount: state.configs.length,
            itemBuilder: (context, index) =>
                _ClientBackupTaskTile(config: state.configs[index]),
          );
        },
      ),
    );
  }

  void _showCreateDialog(BuildContext context) {
    final bloc = context.read<ClientBackupBloc>();
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (_) => BlocProvider.value(
        value: bloc,
        child: const _CreateClientBackupSheet(),
      ),
    );
  }
}

// ==================== Task Tile ====================

class _ClientBackupTaskTile extends StatelessWidget {
  final ClientBackupConfig config;

  const _ClientBackupTaskTile({required this.config});

  @override
  Widget build(BuildContext context) {
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
                  Icons.phone_android_outlined,
                  size: 20,
                  color: config.enabled
                      ? Theme.of(context).colorScheme.primary
                      : Colors.grey,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    config.name,
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                if (config.lastRunAt != null)
                  _StatusChip(
                    label: config.lastBackupCount > 0
                        ? '${config.lastBackupCount} 文件'
                        : '已同步',
                    color: Colors.green,
                  ),
              ],
            ),
            const SizedBox(height: 6),
            _InfoRow(icon: Icons.folder_outlined, text: config.sourcePath),
            if (config.lastRunAt != null)
              _InfoRow(
                  icon: Icons.access_time,
                  text: '上次: ${_formatTime(config.lastRunAt!)}'),
            const SizedBox(height: 6),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                BlocBuilder<ClientBackupBloc, ClientBackupState>(
                  builder: (context, state) {
                    final isRunning =
                        state.runningConfigId == config.id;
                    return TextButton(
                      onPressed: isRunning
                          ? null
                          : () {
                              context.read<ClientBackupBloc>().add(
                                  ClientBackupTriggerEvent(id: config.id));
                            },
                      child: isRunning
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child:
                                  CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('立即执行'),
                    );
                  },
                ),
                TextButton(
                  onPressed: () => _showEditDialog(context),
                  child: const Text('编辑'),
                ),
                TextButton(
                  onPressed: () => _confirmDelete(context),
                  style: TextButton.styleFrom(
                    foregroundColor: Theme.of(context).colorScheme.error,
                  ),
                  child: const Text('删除'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  String _formatTime(DateTime time) {
    return '${time.month}/${time.day} ${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
  }

  void _showEditDialog(BuildContext context) {
    final bloc = context.read<ClientBackupBloc>();
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (_) => BlocProvider.value(
        value: bloc,
        child: _CreateClientBackupSheet(config: config),
      ),
    );
  }

  void _confirmDelete(BuildContext context) {
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除客户端备份任务'),
        content: Text('确定要删除「${config.name}」吗？备份记录也会被清除。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () {
              context
                  .read<ClientBackupBloc>()
                  .add(ClientBackupDeleteEvent(ids: [config.id]));
              Navigator.pop(ctx);
            },
            child: const Text('删除'),
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

// ==================== 客户端备份创建/编辑 Sheet ====================

class _CreateClientBackupSheet extends StatefulWidget {
  final ClientBackupConfig? config;

  const _CreateClientBackupSheet({this.config});

  @override
  State<_CreateClientBackupSheet> createState() =>
      _CreateClientBackupSheetState();
}

class _CreateClientBackupSheetState extends State<_CreateClientBackupSheet> {
  final _formKey = GlobalKey<FormState>();
  late TextEditingController _nameController;
  String? _sourcePath;
  String? _targetDirId;
  String _targetDirName = '';
  late TextEditingController _ignoreController;
  bool _recursive = true;
  bool _enabled = true;

  @override
  void initState() {
    super.initState();
    final config = widget.config;
    _nameController = TextEditingController(text: config?.name ?? '');
    _sourcePath = config?.sourcePath;
    _ignoreController =
        TextEditingController(text: config?.ignoreRules ?? '');
    _recursive = config?.recursive ?? true;
    _enabled = config?.enabled ?? true;
    if (config?.targetDirId != null) {
      _targetDirId = config!.targetDirId;
      _loadTargetDirName(config.targetDirId);
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
    _ignoreController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final isEdit = widget.config != null;
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
              Text(isEdit ? '编辑自动备份' : '创建自动备份',
                  style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 16),
              TextFormField(
                controller: _nameController,
                decoration: const InputDecoration(
                    labelText: '任务名称', border: OutlineInputBorder()),
                validator: (v) => v?.isEmpty == true ? '请输入任务名称' : null,
              ),
              const SizedBox(height: 12),
              // 本地路径选择
              InkWell(
                onTap: _pickSourcePath,
                borderRadius: BorderRadius.circular(12),
                child: InputDecorator(
                  isEmpty: _sourcePath == null,
                  decoration: InputDecoration(
                    labelText: '本地源路径',
                    hintText: _sourcePath == null ? '点击选择本地文件夹' : null,
                    border: const OutlineInputBorder(),
                    suffixIcon: const Icon(Icons.folder_open),
                  ),
                  child: _sourcePath != null
                      ? Text(_sourcePath!, maxLines: 1, overflow: TextOverflow.ellipsis)
                      : null,
                ),
              ),
              const SizedBox(height: 12),
              // 服务端目标目录
              InkWell(
                onTap: _pickTargetFolder,
                borderRadius: BorderRadius.circular(12),
                child: InputDecorator(
                  isEmpty: _targetDirId == null,
                  decoration: InputDecoration(
                    labelText: '服务端目标目录',
                    hintText: _targetDirId == null ? '点击选择目标文件夹' : null,
                    border: const OutlineInputBorder(),
                    suffixIcon: const Icon(Icons.cloud_upload_outlined),
                  ),
                  child: _targetDirId != null
                      ? Text(_targetDirName, maxLines: 1, overflow: TextOverflow.ellipsis)
                      : null,
                ),
              ),
              const SizedBox(height: 12),
              TextFormField(
                controller: _ignoreController,
                decoration: const InputDecoration(
                  labelText: '忽略规则（可选）',
                  hintText: '兼容 .gitignore 语法，每行一条',
                  border: OutlineInputBorder(),
                ),
                maxLines: 3,
              ),
              SwitchListTile(
                  title: const Text('包含子目录'),
                  value: _recursive,
                  onChanged: (v) => setState(() => _recursive = v)),
              SwitchListTile(
                  title: const Text('启用'),
                  value: _enabled,
                  onChanged: (v) => setState(() => _enabled = v)),
              const SizedBox(height: 16),
              FilledButton(
                  onPressed: _submit, child: Text(isEdit ? '保存' : '创建')),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _pickSourcePath() async {
    if (isDesktop()) {
      final path = await FilePicker.platform.getDirectoryPath(
        dialogTitle: '选择备份源文件夹',
      );
      if (path != null && mounted) {
        setState(() => _sourcePath = path);
      }
    } else {
      // 移动端：手动输入路径
      final controller = TextEditingController(text: _sourcePath ?? '');
      final result = await showDialog<String>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('输入源路径'),
          content: TextField(
            controller: controller,
            decoration: const InputDecoration(
              hintText: '/storage/emulated/0/DCIM',
              border: OutlineInputBorder(),
            ),
          ),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('取消')),
            FilledButton(
                onPressed: () => Navigator.pop(ctx, controller.text),
                child: const Text('确定')),
          ],
        ),
      );
      if (result != null && result.isNotEmpty && mounted) {
        setState(() => _sourcePath = result);
      }
    }
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
    if (!_formKey.currentState!.validate()) return;
    if (_sourcePath == null) {
      ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('请选择本地源路径')));
      return;
    }
    if (_targetDirId == null) {
      ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('请选择服务端目标目录')));
      return;
    }

    final bloc = context.read<ClientBackupBloc>();
    if (widget.config != null) {
      bloc.add(ClientBackupUpdateEvent(
        id: widget.config!.id,
        name: _nameController.text,
        sourcePath: _sourcePath,
        targetDirId: _targetDirId,
        ignoreRules:
            _ignoreController.text.isEmpty ? null : _ignoreController.text,
        recursive: _recursive,
        enabled: _enabled,
      ));
    } else {
      bloc.add(ClientBackupCreateEvent(
        id: const Uuid().v4(),
        name: _nameController.text,
        sourcePath: _sourcePath!,
        targetDirId: _targetDirId!,
        ignoreRules:
            _ignoreController.text.isEmpty ? null : _ignoreController.text,
        recursive: _recursive,
        enabled: _enabled,
      ));
    }
    Navigator.pop(context);
  }
}
