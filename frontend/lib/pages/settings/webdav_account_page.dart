import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/pages/files/folder_picker_page.dart';
import 'package:pfm/proto/native/webdav.pb.dart';

/// WebDAV 账号管理页
///
/// 每个账号 = 独立用户名/密码 + 绑定的一个目录 + 只读开关。
/// 第三方客户端用该凭据访问 /dav 时，只能看到、管理绑定目录内的文件
/// （客户端看到的 "/" 就是绑定目录，路径相对它解析）。
/// 明文密码只在创建/重置那一次由服务端返回，之后不可再查看。
class WebdavAccountPage extends StatefulWidget {
  const WebdavAccountPage({super.key});

  @override
  State<WebdavAccountPage> createState() => _WebdavAccountPageState();
}

class _WebdavAccountPageState extends State<WebdavAccountPage> {
  final _repo = FmRepository();

  bool _loading = true;
  String? _error;
  List<WebdavAccountInfo> _accounts = const [];
  String _davUrl = '/dav';

  @override
  void initState() {
    super.initState();
    _load();
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    final result = await _repo.listWebdavAccounts();
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (result.success && result.data != null) {
        _accounts = result.data!.accounts;
        if (result.data!.davUrl.isNotEmpty) _davUrl = result.data!.davUrl;
      } else {
        _error = result.message ?? AppLocalizations.of(context).webdavAccount_loadFailed;
      }
    });
  }

  /// 服务地址（前端拼上当前服务器地址）
  String get _serverUrl {
    final base = _repo.serverBaseUrl;
    if (base.isEmpty) return _davUrl;
    return '$base$_davUrl';
  }

  // ---------------- 操作 ----------------

  Future<void> _copyServerUrl() async {
    final al = AppLocalizations.of(context);
    await Clipboard.setData(ClipboardData(text: _serverUrl));
    _toast(al.webdavAccount_copied);
  }

  /// 创建账号：成功后弹出一次性密码
  Future<void> _create() async {
    final al = AppLocalizations.of(context);
    final draft = await showDialog<_WebdavAccountDraft>(
      context: context,
      builder: (_) => const _WebdavAccountFormDialog(),
    );
    if (draft == null || !mounted) return;

    final result = await _repo.createWebdavAccount(
      rootFileId: draft.rootFileId,
      name: draft.name,
      username: draft.username,
      password: draft.password,
      readOnly: draft.readOnly,
    );
    if (!mounted) return;
    if (!result.success || result.data == null) {
      _toast(result.message ?? al.webdavAccount_createFailed);
      return;
    }
    await _load();
    if (!mounted) return;
    await _showPasswordDialog(result.data!.password);
    _toast(al.webdavAccount_created);
  }

  /// 编辑账号（标签 / 绑定目录 / 只读 / 启用）
  Future<void> _edit(WebdavAccountInfo account) async {
    final al = AppLocalizations.of(context);
    final draft = await showDialog<_WebdavAccountDraft>(
      context: context,
      builder: (_) => _WebdavAccountFormDialog(
        account: account,
        initialFolderId: account.rootFileId,
      ),
    );
    if (draft == null || !mounted) return;

    final result = await _repo.updateWebdavAccount(
      id: account.id,
      name: draft.name,
      rootFileId: draft.rootFileId,
      readOnly: draft.readOnly,
      enabled: draft.enabled,
    );
    if (!mounted) return;
    if (!result.success) {
      _toast(result.message ?? al.webdavAccount_updateFailed);
      return;
    }
    await _load();
    _toast(al.webdavAccount_updated);
  }

  /// 重置密码：成功后弹出一次性密码
  Future<void> _resetPassword(WebdavAccountInfo account) async {
    final al = AppLocalizations.of(context);
    final ok = await _confirm(al.webdavAccount_resetPassword, al.webdavAccount_resetConfirm);
    if (!ok || !mounted) return;

    final result = await _repo.resetWebdavAccountPassword(id: account.id);
    if (!mounted) return;
    if (!result.success || result.data == null) {
      _toast(result.message ?? al.webdavAccount_resetFailed);
      return;
    }
    await _showPasswordDialog(result.data!.password);
    _toast(al.webdavAccount_resetDone);
  }

  Future<void> _delete(WebdavAccountInfo account) async {
    final al = AppLocalizations.of(context);
    final ok = await _confirm(al.common_delete, al.webdavAccount_deleteConfirm);
    if (!ok || !mounted) return;

    final result = await _repo.deleteWebdavAccount(id: account.id);
    if (!mounted) return;
    if (!result.success) {
      _toast(result.message ?? al.webdavAccount_deleteFailed);
      return;
    }
    await _load();
    _toast(al.webdavAccount_deleted);
  }

  Future<bool> _confirm(String title, String content) async {
    final al = AppLocalizations.of(context);
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: Text(content),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: Text(al.common_confirm),
          ),
        ],
      ),
    );
    return ok ?? false;
  }

  /// 展示一次性明文密码（服务端只存哈希，关闭后无法再查看）
  Future<void> _showPasswordDialog(String password) async {
    final al = AppLocalizations.of(context);
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.webdavAccount_passwordLabel),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(al.webdavAccount_passwordOnce),
            const SizedBox(height: PfmSpacing.md),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(PfmSpacing.md),
              decoration: BoxDecoration(
                color: Theme.of(ctx).colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(8),
              ),
              child: SelectableText(
                password,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: password));
              if (ctx.mounted) Navigator.of(ctx).pop();
              _toast(al.webdavAccount_copied);
            },
            child: Text(al.webdavAccount_copy),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: Text(al.common_close),
          ),
        ],
      ),
    );
  }

  // ---------------- UI ----------------

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);

    return Scaffold(
      appBar: AppBar(
        title: Text(al.webdavAccount_title),
        actions: [
          IconButton(
            icon: const Icon(Icons.add),
            tooltip: al.webdavAccount_add,
            onPressed: _create,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: _buildBody(al),
      ),
    );
  }

  Widget _buildBody(AppLocalizations al) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return ListView(
        padding: const EdgeInsets.all(PfmSpacing.lg),
        children: [
          const SizedBox(height: PfmSpacing.xl),
          Icon(Icons.error_outline, size: 48, color: Theme.of(context).colorScheme.error),
          const SizedBox(height: PfmSpacing.md),
          Text(_error!, textAlign: TextAlign.center),
          const SizedBox(height: PfmSpacing.md),
          Center(
            child: FilledButton.icon(
              onPressed: _load,
              icon: const Icon(Icons.refresh),
              label: Text(al.common_retry),
            ),
          ),
        ],
      );
    }

    return ListView(
      padding: const EdgeInsets.all(PfmSpacing.lg),
      children: [
        _buildServerCard(al),
        const SizedBox(height: PfmSpacing.lg),
        if (_accounts.isEmpty) _buildEmpty(al) else ..._accounts.map(_buildAccountCard),
      ],
    );
  }

  Widget _buildServerCard(AppLocalizations al) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(PfmSpacing.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.cloud_outlined),
                const SizedBox(width: PfmSpacing.sm),
                Text(al.webdavAccount_serverUrl, style: Theme.of(context).textTheme.titleSmall),
              ],
            ),
            const SizedBox(height: PfmSpacing.sm),
            SelectableText(_serverUrl, style: const TextStyle(fontFamily: 'monospace')),
            const SizedBox(height: PfmSpacing.sm),
            Text(al.webdavAccount_serverUrlHint, style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: PfmSpacing.xs),
            Text(al.webdavAccount_isolationNote, style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: PfmSpacing.xs),
            Text(al.webdavAccount_pathNote, style: Theme.of(context).textTheme.bodySmall),
            const SizedBox(height: PfmSpacing.sm),
            Align(
              alignment: Alignment.centerRight,
              child: TextButton.icon(
                onPressed: _copyServerUrl,
                icon: const Icon(Icons.copy, size: 18),
                label: Text(al.webdavAccount_copy),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEmpty(AppLocalizations al) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: PfmSpacing.xl),
      child: Column(
        children: [
          Icon(
            Icons.key_outlined,
            size: 48,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: PfmSpacing.md),
          Text(al.webdavAccount_empty, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: PfmSpacing.sm),
          Text(
            al.webdavAccount_emptyHint,
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: PfmSpacing.lg),
          FilledButton.icon(
            onPressed: _create,
            icon: const Icon(Icons.add),
            label: Text(al.webdavAccount_add),
          ),
        ],
      ),
    );
  }

  Widget _buildAccountCard(WebdavAccountInfo account) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;
    final title = account.name.isNotEmpty ? account.name : account.username;

    return Card(
      margin: const EdgeInsets.only(bottom: PfmSpacing.md),
      child: ListTile(
        contentPadding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.sm),
        leading: Icon(
          account.readOnly ? Icons.visibility_outlined : Icons.sync_outlined,
          color: account.enabled ? PfmColors.primary : colorScheme.onSurfaceVariant,
        ),
        title: Text(title),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const SizedBox(height: PfmSpacing.xs),
            Text('${al.webdavAccount_username}: ${account.username}'),
            Text('${al.webdavAccount_folder}: ${account.hasRootPath() ? account.rootPath : '-'}'),
            const SizedBox(height: PfmSpacing.xs),
            Wrap(
              spacing: PfmSpacing.sm,
              runSpacing: PfmSpacing.xs,
              children: [
                _chip(
                  account.readOnly
                      ? al.webdavAccount_readOnly
                      : al.webdavAccount_readWrite,
                  colorScheme.primary,
                ),
                if (!account.enabled)
                  _chip(al.webdavAccount_disabled, colorScheme.error),
                if (!account.rootValid)
                  _chip(al.webdavAccount_rootInvalid, colorScheme.error),
                _chip(
                  account.hasLastUsedTime() && account.lastUsedTime.isNotEmpty
                      ? '${al.webdavAccount_lastUsed}: ${account.lastUsedTime}'
                      : al.webdavAccount_neverUsed,
                  colorScheme.onSurfaceVariant,
                ),
              ],
            ),
            if (account.disabledReason.isNotEmpty) ...[
              const SizedBox(height: PfmSpacing.xs),
              Text(
                account.disabledReason,
                style: TextStyle(fontSize: 12, color: colorScheme.error),
              ),
            ],
          ],
        ),
        trailing: PopupMenuButton<String>(
          onSelected: (value) {
            switch (value) {
              case 'edit':
                _edit(account);
                break;
              case 'reset':
                _resetPassword(account);
                break;
              case 'delete':
                _delete(account);
                break;
            }
          },
          itemBuilder: (_) => [
            PopupMenuItem(value: 'edit', child: Text(al.common_edit)),
            PopupMenuItem(value: 'reset', child: Text(al.webdavAccount_resetPassword)),
            PopupMenuItem(value: 'delete', child: Text(al.common_delete)),
          ],
        ),
        onTap: () => _edit(account),
      ),
    );
  }

  Widget _chip(String text, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Text(text, style: TextStyle(fontSize: 12, color: color)),
    );
  }
}

/// 表单结果（创建与编辑共用）
class _WebdavAccountDraft {
  final String name;
  final String username;
  final String password;
  final String rootFileId;
  final bool readOnly;
  final bool enabled;

  const _WebdavAccountDraft({
    required this.name,
    required this.username,
    required this.password,
    required this.rootFileId,
    required this.readOnly,
    required this.enabled,
  });
}

/// 新建 / 编辑表单
///
/// [account] 为空 = 新建（显示用户名/密码输入）；
/// 非空 = 编辑（用户名与密码不可改，改密码走「重置密码」）。
class _WebdavAccountFormDialog extends StatefulWidget {
  const _WebdavAccountFormDialog({this.account, this.initialFolderId = ''});

  final WebdavAccountInfo? account;
  final String initialFolderId;

  @override
  State<_WebdavAccountFormDialog> createState() => _WebdavAccountFormDialogState();
}

class _WebdavAccountFormDialogState extends State<_WebdavAccountFormDialog> {
  final _nameCtrl = TextEditingController();
  final _usernameCtrl = TextEditingController();
  final _passwordCtrl = TextEditingController();

  late String _rootFileId;
  String _rootPath = '';
  late bool _readOnly;
  late bool _enabled;

  bool get _isCreate => widget.account == null;

  @override
  void initState() {
    super.initState();
    final account = widget.account;
    _nameCtrl.text = account?.name ?? '';
    _usernameCtrl.text = account?.username ?? '';
    _rootFileId = widget.initialFolderId;
    _rootPath = account?.rootPath ?? '';
    _readOnly = account?.readOnly ?? false;
    _enabled = account?.enabled ?? true;
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _usernameCtrl.dispose();
    _passwordCtrl.dispose();
    super.dispose();
  }

  Future<void> _pickFolder() async {
    final picked = await Navigator.of(context).push<FolderPickResult>(
      MaterialPageRoute(
        builder: (_) => FolderPickerPage(initialFolderId: _rootFileId),
      ),
    );
    if (picked == null || !mounted) return;
    setState(() {
      _rootFileId = picked.id;
      _rootPath = picked.name;
    });
  }

  void _submit() {
    final al = AppLocalizations.of(context);
    if (_rootFileId.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(al.webdavAccount_folderRequired)),
      );
      return;
    }
    Navigator.of(context).pop(
      _WebdavAccountDraft(
        name: _nameCtrl.text.trim(),
        username: _usernameCtrl.text.trim(),
        password: _passwordCtrl.text,
        rootFileId: _rootFileId,
        readOnly: _readOnly,
        enabled: _enabled,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);
    final colorScheme = Theme.of(context).colorScheme;

    return AlertDialog(
      title: Text(_isCreate ? al.webdavAccount_createTitle : al.webdavAccount_editTitle),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _nameCtrl,
              maxLength: 64,
              decoration: InputDecoration(
                labelText: al.webdavAccount_nameLabel,
                helperText: al.webdavAccount_nameHint,
                counterText: '',
              ),
            ),
            if (_isCreate) ...[
              TextField(
                controller: _usernameCtrl,
                maxLength: 64,
                decoration: InputDecoration(
                  labelText: al.webdavAccount_usernameLabel,
                  helperText: al.webdavAccount_usernameHint,
                  counterText: '',
                ),
              ),
              TextField(
                controller: _passwordCtrl,
                maxLength: 128,
                obscureText: true,
                decoration: InputDecoration(
                  labelText: al.webdavAccount_passwordLabel,
                  helperText: al.webdavAccount_passwordHint,
                  counterText: '',
                ),
              ),
            ],
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.folder_outlined),
              title: Text(al.webdavAccount_selectFolder),
              subtitle: Text(
                _rootPath.isEmpty
                    ? (_rootFileId.isEmpty
                          ? al.webdavAccount_folderNotSelected
                          : _rootFileId)
                    : _rootPath,
                style: TextStyle(color: colorScheme.onSurfaceVariant),
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: _pickFolder,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              value: _readOnly,
              onChanged: (v) => setState(() => _readOnly = v),
              title: Text(al.webdavAccount_readOnlyLabel),
              subtitle: Text(
                al.webdavAccount_readOnlyHint,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
            if (!_isCreate)
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                value: _enabled,
                onChanged: (v) => setState(() => _enabled = v),
                title: Text(al.webdavAccount_enabledLabel),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text(al.common_cancel),
        ),
        FilledButton(
          onPressed: _submit,
          child: Text(_isCreate ? al.common_create : al.common_save),
        ),
      ],
    );
  }
}
