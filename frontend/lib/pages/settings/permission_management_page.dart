import 'package:flutter/material.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/proto/native/permission.pb.dart';
import 'package:pfm/proto/native/file.pb.dart';

/// 权限管理页面（管理员专用）
/// 页面1: 用户列表 → 点击进入用户权限详情
class PermissionManagementPage extends StatefulWidget {
  const PermissionManagementPage({super.key});

  @override
  State<PermissionManagementPage> createState() =>
      _PermissionManagementPageState();
}

class _PermissionManagementPageState extends State<PermissionManagementPage> {
  late AppLocalizations al;
  final FmRepository _repo = FmRepository();

  List<CasdoorUserInfo> _users = [];
  bool _isLoading = false;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadUsers();
  }

  Future<void> _loadUsers() async {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    final result = await _repo.listUsers();
    setState(() {
      _isLoading = false;
      if (result.success && result.data != null) {
        _users = result.data!.users;
      } else {
        _errorMessage = result.message ?? '获取用户列表失败';
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    al = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(al.permission_title),
        centerTitle: true,
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _loadUsers,
            tooltip: al.permission_refresh,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_errorMessage != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_errorMessage!, style: const TextStyle(color: PfmColors.error)),
            const SizedBox(height: 16),
            ElevatedButton(onPressed: _loadUsers, child: Text(al.permission_retry)),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: _users.length,
      itemBuilder: (context, index) {
        final user = _users[index];
        final hasRole = user.roles.isNotEmpty;

        return ListTile(
          leading: CircleAvatar(
            backgroundColor:
                hasRole ? Theme.of(context).colorScheme.primary : Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5),
            child: Text(
              user.userId.isNotEmpty ? user.userId[0].toUpperCase() : '?',
              style:
                  const TextStyle(color: Colors.white, fontWeight: FontWeight.bold),
            ),
          ),
          title: Text(user.userId),
          subtitle: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (user.email.isNotEmpty) Text(user.email),
              if (hasRole)
                Wrap(
                  spacing: 4,
                  children: user.roles
                      .map((r) => Chip(
                            label: Text(r, style: const TextStyle(fontSize: 10)),
                            padding: EdgeInsets.zero,
                            materialTapTargetSize:
                                MaterialTapTargetSize.shrinkWrap,
                            visualDensity: VisualDensity.compact,
                          ))
                      .toList(),
                )
              else
                Text(al.permission_noRoles,
                    style: TextStyle(fontSize: 12, color: PfmColors.warning)),
            ],
          ),
          trailing: const Icon(Icons.chevron_right),
          onTap: () {
            Navigator.push(
              context,
              MaterialPageRoute(
                builder: (_) => UserPermissionDetailPage(user: user),
              ),
            );
          },
        );
      },
    );
  }
}

/// 页面2: 用户权限详情 + 授权入口
class UserPermissionDetailPage extends StatefulWidget {
  final CasdoorUserInfo user;
  const UserPermissionDetailPage({super.key, required this.user});

  @override
  State<UserPermissionDetailPage> createState() =>
      _UserPermissionDetailPageState();
}

class _UserPermissionDetailPageState extends State<UserPermissionDetailPage> {
  late AppLocalizations al;
  final FmRepository _repo = FmRepository();
  List<DirectoryPermissionInfo> _permissions = [];
  bool _isLoading = false;

  static const int permRead = 1;
  static const int permUpload = 2;
  static const int permMkdir = 4;
  static const int permDelete = 8;
  static const int permMove = 16;
  static const int permRename = 32;

  @override
  void initState() {
    super.initState();
    _loadPermissions();
  }

  Future<void> _loadPermissions() async {
    setState(() => _isLoading = true);
    final result = await _repo.getUserPermissions(userId: widget.user.userId);
    setState(() {
      _isLoading = false;
      if (result.success && result.data != null) {
        _permissions = result.data!.permissions;
      }
    });
  }

  Future<void> _openGrantFlow() async {
    final result = await Navigator.push<_GrantResult>(
      context,
      MaterialPageRoute(
        builder: (_) => DirectoryPickerPage(repo: _repo),
      ),
    );

    if (result == null || !mounted) return;

    final grantResult = await _repo.grantDirectoryPermission(
      userId: widget.user.userId,
      fileId: result.fileId,
      permissions: result.permissions,
      alias: result.alias,
    );

    if (grantResult.success) {
      _loadPermissions();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.permission_grantSuccess)),
        );
      }
    } else {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.permission_grantFailed}: ${grantResult.message}')),
        );
      }
    }
  }

  Future<void> _revokePermission(DirectoryPermissionInfo perm) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.permission_confirmRevoke),
        content: Text(al.permission_confirmRevokeMsg(perm.fileName)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(AppLocalizations.of(context).common_cancel),
          ),
          ElevatedButton(
            onPressed: () => Navigator.pop(ctx, true),
            style: ElevatedButton.styleFrom(backgroundColor: PfmColors.error),
            child: Text(al.permission_revoke),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    final result = await _repo.revokeDirectoryPermission(
      userId: widget.user.userId,
      fileId: perm.fileId,
    );

    if (result.success) {
      _loadPermissions();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(al.permission_revokeSuccess)),
        );
      }
    } else {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('${al.permission_revokeFailed}: ${result.message}')),
        );
      }
    }
  }

  String _permName(int perm) {
    switch (perm) {
      case permRead:
        return al.permission_read;
      case permUpload:
        return al.permission_upload;
      case permMkdir:
        return al.permission_createDir;
      case permDelete:
        return al.permission_delete;
      case permMove:
        return al.permission_move;
      case permRename:
        return al.permission_rename;
      default:
        return '未知';
    }
  }

  List<int> _permList(int permissions) {
    final List<int> perms = [];
    if (permissions & permRead != 0) perms.add(permRead);
    if (permissions & permUpload != 0) perms.add(permUpload);
    if (permissions & permMkdir != 0) perms.add(permMkdir);
    if (permissions & permDelete != 0) perms.add(permDelete);
    if (permissions & permMove != 0) perms.add(permMove);
    if (permissions & permRename != 0) perms.add(permRename);
    return perms;
  }

  @override
  Widget build(BuildContext context) {
    al = AppLocalizations.of(context)!;
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.user.userId),
        centerTitle: true,
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _openGrantFlow,
        icon: const Icon(Icons.add),
        label: Text(al.permission_grant),
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : _permissions.isEmpty
              ? Center(
                  child: Text(al.permission_noDirPermissions,
                      style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5), fontSize: 16)))
              : ListView.builder(
                  padding: const EdgeInsets.only(bottom: 80),
                  itemCount: _permissions.length,
                  itemBuilder: (context, index) {
                    final perm = _permissions[index];
                    final permList = _permList(perm.permissions);

                    return Card(
                      margin: const EdgeInsets.symmetric(
                          horizontal: 12, vertical: 4),
                      child: ListTile(
                        leading: Icon(Icons.folder,
                            color: Theme.of(context).colorScheme.primary),
                        title: Text(
                          perm.alias.isNotEmpty
                              ? perm.alias
                              : perm.fileName.isNotEmpty
                                  ? perm.fileName
                                  : '目录 ${perm.fileId}',
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                        subtitle: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (perm.filePath.isNotEmpty)
                              Text(perm.filePath,
                                  style: TextStyle(
                                      fontSize: 12, color: Theme.of(context).colorScheme.onSurfaceVariant)),
                            const SizedBox(height: 4),
                            Wrap(
                              spacing: 4,
                              children: permList
                                  .map((p) => Chip(
                                        label: Text(_permName(p),
                                            style:
                                                const TextStyle(fontSize: 11)),
                                        backgroundColor: Theme.of(context)
                                            .colorScheme
                                            .primaryContainer,
                                        padding: EdgeInsets.zero,
                                        materialTapTargetSize:
                                            MaterialTapTargetSize.shrinkWrap,
                                        visualDensity: VisualDensity.compact,
                                      ))
                                  .toList(),
                            ),
                          ],
                        ),
                        trailing: IconButton(
                          icon: const Icon(Icons.delete, color: PfmColors.error),
                          onPressed: () => _revokePermission(perm),
                          tooltip: '撤销',
                        ),
                      ),
                    );
                  },
                ),
    );
  }
}

/// 授权结果
class _GrantResult {
  final String fileId;
  final int permissions;
  final String alias;
  _GrantResult(this.fileId, this.permissions, this.alias);
}

/// 页面3: 目录选择器
/// 交互：点击文件夹进入子目录，浏览到目标目录后点"选择此目录"授权
class DirectoryPickerPage extends StatefulWidget {
  final FmRepository repo;
  const DirectoryPickerPage({super.key, required this.repo});

  @override
  State<DirectoryPickerPage> createState() => _DirectoryPickerPageState();
}

class _DirectoryPickerPageState extends State<DirectoryPickerPage> {
  late AppLocalizations al;
  late List<_Crumb> _crumbs;

  List<File> _folders = [];
  bool _isLoading = false;

  // 权限
  static const int permRead = 1;
  static const int permUpload = 2;
  static const int permMkdir = 4;
  static const int permDelete = 8;
  static const int permMove = 16;
  static const int permRename = 32;
  int _perms = permRead;

  // 别名
  final TextEditingController _aliasController = TextEditingController();

  @override
  void dispose() {
    _aliasController.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _crumbs = [_Crumb(id: '', name: 'Root')];  // Will be localized in build
    _loadFolders('');
  }

  String get _currentId => _crumbs.last.id;
  String get _currentName => _crumbs.last.name;
  bool get _isRoot => _crumbs.length == 1;

  Future<void> _loadFolders(String parentId) async {
    setState(() => _isLoading = true);
    final result = await widget.repo.getFolderList(parentId: parentId);
    setState(() {
      _isLoading = false;
      if (result.success && result.data != null) {
        _folders = result.data!.folders;
      } else {
        _folders = [];
      }
    });
  }

  void _enter(File folder) {
    setState(() {
      _crumbs.add(_Crumb(id: folder.id, name: folder.name));
    });
    _loadFolders(folder.id);
  }

  void _goBack() {
    if (_crumbs.length <= 1) return;
    setState(() {
      _crumbs.removeLast();
    });
    _loadFolders(_currentId);
  }

  void _goTo(int index) {
    setState(() {
      _crumbs.removeRange(index + 1, _crumbs.length);
    });
    _loadFolders(_currentId);
  }

  void _confirm() {
    Navigator.pop(context, _GrantResult(_currentId, _perms, _aliasController.text.trim()));
  }

  Widget _chip(String label, int bit) {
    final on = _perms & bit != 0;
    return FilterChip(
      label: Text(label),
      selected: on,
      onSelected: (v) {
        setState(() {
          _perms = v ? _perms | bit : _perms & ~bit;
        });
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    al = AppLocalizations.of(context)!;
    if (_crumbs.isEmpty) {
      _crumbs = [_Crumb(id: '', name: al.permission_rootDirectory)];
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(_isRoot ? al.permission_selectDirectory : _currentName),
        centerTitle: true,
      ),
      // 底部操作栏
      bottomNavigationBar: Container(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surface,
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.08),
              blurRadius: 8,
              offset: const Offset(0, -2),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // 当前目录
            Text(
              '将授权目录: $_currentName',
              style: TextStyle(
                fontWeight: FontWeight.bold,
                color: Theme.of(context).colorScheme.primary,
              ),
            ),
            if (_crumbs.length > 1)
              Text(
                _crumbs.map((c) => c.name).join(' / '),
                style: TextStyle(fontSize: 12, color: Theme.of(context).colorScheme.onSurfaceVariant),
              ),
            const SizedBox(height: 8),
            // 权限
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                _chip(al.permission_read, permRead),
                _chip(al.permission_upload, permUpload),
                _chip(al.permission_createDir, permMkdir),
                _chip(al.permission_delete, permDelete),
                _chip(al.permission_move, permMove),
                _chip(al.permission_rename, permRename),
              ],
            ),
            const SizedBox(height: 8),
            // 别名输入
            TextField(
              controller: _aliasController,
              decoration: InputDecoration(
                labelText: al.permission_aliasLabel,
                hintText: al.permission_aliasHint,
                isDense: true,
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(8),
                ),
                contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              ),
              maxLength: 32,
              buildCounter: (context, {required currentLength, required isFocused, required maxLength}) => null,
            ),
            const SizedBox(height: 12),
            ElevatedButton(
              onPressed: _confirm,
              style: ElevatedButton.styleFrom(
                minimumSize: const Size.fromHeight(44),
              ),
              child: Text(al.permission_selectAndGrant),
            ),
          ],
        ),
      ),
      body: Column(
        children: [
          // 面包屑
          Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            color: Theme.of(context)
                .colorScheme
                .surfaceContainerHighest
                .withValues(alpha: 0.5),
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (int i = 0; i < _crumbs.length; i++) ...[
                    if (i > 0)
                      Icon(Icons.chevron_right,
                          size: 16, color: Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5)),
                    InkWell(
                      onTap: () => _goTo(i),
                      child: Text(
                        _crumbs[i].name,
                        style: TextStyle(
                          fontSize: 13,
                          color: i == _crumbs.length - 1
                              ? Theme.of(context).colorScheme.primary
                              : Theme.of(context).colorScheme.onSurfaceVariant,
                          fontWeight: i == _crumbs.length - 1
                              ? FontWeight.bold
                              : FontWeight.normal,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
          // 子文件夹列表
          Expanded(
            child: _isLoading
                ? const Center(child: CircularProgressIndicator())
                : _folders.isEmpty
                    ? Center(
                        child: Text(al.permission_noSubfolders,
                            style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5))))
                    : ListView.builder(
                        itemCount: _folders.length,
                        itemBuilder: (context, index) {
                          final folder = _folders[index];
                          return ListTile(
                            leading: Icon(Icons.folder,
                                color: PfmColors.warning),
                            title: Text(folder.name),
                            trailing: Icon(Icons.chevron_right,
                                color: Theme.of(context).colorScheme.onSurfaceVariant.withValues(alpha: 0.5)),
                            onTap: () => _enter(folder),
                          );
                        },
                      ),
          ),
        ],
      ),
    );
  }
}

class _Crumb {
  final String id;
  final String name;
  _Crumb({required this.id, required this.name});
}
