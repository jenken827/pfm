import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/fm_repository.dart';
import 'package:pfm/design/design_tokens.dart';
import 'package:pfm/l10n/app_localizations.dart';
import 'package:pfm/models/models.dart';
import 'package:pfm/services/services.dart';

/// 个人资料编辑页：修改用户名 / 邮箱 / 头像
///
/// 仅能修改当前登录用户自己的资料（服务端按 token 判定，不接受指定他人）。
/// 头像经客户端选图 → 上传 → 服务端裁剪为 256×256 正方形 JPEG。
class ProfileEditPage extends StatefulWidget {
  const ProfileEditPage({super.key});

  @override
  State<ProfileEditPage> createState() => _ProfileEditPageState();
}

class _ProfileEditPageState extends State<ProfileEditPage> {
  final _formKey = GlobalKey<FormState>();
  final _usernameCtrl = TextEditingController();
  final _emailCtrl = TextEditingController();
  final _repo = FmRepository();

  bool _saving = false;
  bool _uploadingAvatar = false;
  String? _avatarUrl;

  @override
  void initState() {
    super.initState();
    final user = GetIt.I.get<LocalStorage>().user;
    _usernameCtrl.text = user?.name ?? '';
    _emailCtrl.text = user?.email ?? '';
    _avatarUrl = _repo.resolveAvatarUrl(user?.avatar);
  }

  @override
  void dispose() {
    _usernameCtrl.dispose();
    _emailCtrl.dispose();
    super.dispose();
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  /// 选择并上传头像
  Future<void> _pickAndUploadAvatar() async {
    final al = AppLocalizations.of(context);
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.image,
      withData: false,
    );
    final path = picked?.files.single.path;
    if (path == null) return;

    setState(() => _uploadingAvatar = true);
    final result = await _repo.uploadAvatar(filePath: path);
    if (!mounted) return;
    setState(() => _uploadingAvatar = false);

    if (!result.success || result.data == null) {
      _toast(result.message ?? al.profile_avatarUploadFailed);
      return;
    }

    // 服务端返回新的相对路径（带 ?v= 破缓存）
    final newPath = result.data!.avatar;
    await _persistLocal(avatar: newPath);
    if (!mounted) return;
    setState(() => _avatarUrl = _repo.resolveAvatarUrl(newPath));
    _toast(al.profile_avatarUpdated);
  }

  /// 保存用户名 / 邮箱
  Future<void> _save() async {
    final al = AppLocalizations.of(context);
    if (!(_formKey.currentState?.validate() ?? false)) return;

    setState(() => _saving = true);
    final result = await _repo.updateProfile(
      username: _usernameCtrl.text.trim(),
      email: _emailCtrl.text.trim(),
    );
    if (!mounted) return;
    setState(() => _saving = false);

    if (!result.success || result.data == null) {
      _toast(result.message ?? al.profile_saveFailed);
      return;
    }

    await _persistLocal(
      username: result.data!.username,
      email: result.data!.email,
    );
    _toast(al.profile_saved);
    if (mounted) Navigator.of(context).pop(true);
  }

  /// 把变更写回本地存储，保证设置页立即反映（不重新登录）
  Future<void> _persistLocal({String? username, String? email, String? avatar}) async {
    final ls = GetIt.I.get<LocalStorage>();
    final cur = ls.user;
    if (cur == null) return;
    await ls.saveUser(
      User(
        id: cur.id,
        name: username ?? cur.name,
        createdTime: cur.createdTime,
        updatedTime: DateTime.now(),
        avatar: avatar ?? cur.avatar,
        email: email ?? cur.email,
        phone: cur.phone,
        roles: cur.roles,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final al = AppLocalizations.of(context);

    return Scaffold(
      appBar: AppBar(title: Text(al.profile_title)),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.all(PfmSpacing.lg),
          children: [
            Center(child: _buildAvatar()),
            const SizedBox(height: PfmSpacing.xl),
            TextFormField(
              controller: _usernameCtrl,
              decoration: InputDecoration(
                labelText: al.profile_username,
                prefixIcon: const Icon(Icons.person_outline),
              ),
              maxLength: 64,
              validator: (v) {
                if (v == null || v.trim().isEmpty) {
                  return al.profile_usernameRequired;
                }
                return null;
              },
            ),
            const SizedBox(height: PfmSpacing.md),
            TextFormField(
              controller: _emailCtrl,
              decoration: InputDecoration(
                labelText: al.profile_email,
                prefixIcon: const Icon(Icons.mail_outline),
              ),
              maxLength: 128,
              keyboardType: TextInputType.emailAddress,
            ),
            const SizedBox(height: PfmSpacing.xl),
            FilledButton(
              onPressed: _saving ? null : _save,
              child: _saving
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(al.common_save),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildAvatar() {
    final colorScheme = Theme.of(context).colorScheme;

    return Stack(
      children: [
        CircleAvatar(
          radius: 48,
          backgroundColor: colorScheme.surfaceContainerHighest,
          backgroundImage: _avatarUrl != null ? NetworkImage(_avatarUrl!) : null,
          child: _avatarUrl == null
              ? Icon(
                  Icons.person,
                  size: 44,
                  color: colorScheme.onSurfaceVariant,
                )
              : null,
        ),
        Positioned(
          right: 0,
          bottom: 0,
          child: Material(
            color: PfmColors.primary,
            shape: const CircleBorder(),
            child: InkWell(
              customBorder: const CircleBorder(),
              onTap: _uploadingAvatar ? null : _pickAndUploadAvatar,
              child: Padding(
                padding: const EdgeInsets.all(6),
                child: _uploadingAvatar
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Colors.white,
                        ),
                      )
                    : const Icon(
                        Icons.photo_camera,
                        size: 16,
                        color: Colors.white,
                      ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}
