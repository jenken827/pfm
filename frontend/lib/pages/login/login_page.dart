import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/api/api.dart';
import '../../models/models.dart';
import '../../router/router.dart';
import '../../services/services.dart';
import '../../utils/utils.dart';
import 'package:pfm/l10n/app_localizations.dart';

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  bool _isLoading = false;
  // 认证模式：默认 casdoor（兼容未配置过服务器的旧客户端），
  // 打开页面时从服务器 /api/auth/config 刷新
  String _authMode = AuthMode.casdoor;
  final _ls = GetIt.I.get<LocalStorage>();
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();

  @override
  void initState() {
    super.initState();
    _authMode = _ls.authMode ?? AuthMode.casdoor;
    _refreshAuthMode();
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  /// 从服务器获取认证模式（非阻塞刷新，失败时保持本地缓存值）
  Future<void> _refreshAuthMode() async {
    if (!_ls.fileServerConfigured) return;
    final result = await FmRepository().getAuthConfig();
    if (mounted && result.success && result.data != null) {
      setState(() => _authMode = result.data!);
    }
  }

  bool get _isLocalMode => _authMode == AuthMode.local;

  /// 本地认证登录（auth.mode=local）
  Future<void> _handleLocalLogin() async {
    final username = _usernameController.text.trim();
    final password = _passwordController.text;
    if (username.isEmpty || password.isEmpty || _isLoading) return;

    setState(() => _isLoading = true);
    try {
      final result = await FmRepository().loginWithPassword(
        username: username,
        password: password,
      );
      if (!result.success || result.data == null) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(result.message ?? 'Login failed')),
          );
        }
        return;
      }

      final login = result.data!;
      // 保存 token 与用户信息（复用 Casdoor 存储结构，后续逻辑无感）
      final token = CasdoorToken()..accessToken = login.token;
      await _ls.saveToken(token);
      final user = User(
        id: login.userId,
        name: login.name,
        email: login.email.isEmpty ? null : login.email,
        roles: login.isAdmin ? ['pfm_admin'] : ['pfm_user'],
      );
      await _ls.saveUser(user);
      // 异步获取传输服务配置（非阻塞）
      FmRepository().fetchTransferConfig();

      if (mounted) {
        _navigateToHome();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Login failed: $e')),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isLoading = false);
      }
    }
  }

  /// Casdoor 登录（桌面端浏览器 / 移动端 WebView）
  Future<void> _handleLogin() async {
    if (_isLoading) return;

    setState(() => _isLoading = true);

    try {
      User? user;

      if (isDesktop()) {
        // 桌面端：使用浏览器登录
        final oauthService = DesktopOAuthService();
        user = await oauthService.login();
        oauthService.dispose();
      } else if (isMobile()) {
        // 移动端：使用 WebView 登录
        user = await showCasdoorWebViewLogin(context);
      }

      if (user != null && mounted) {
        _navigateToHome();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Login failed: $e')));
      }
    } finally {
      if (mounted) {
        setState(() => _isLoading = false);
      }
    }
  }

  void _navigateToHome() {
    const HomeRoute().go(context);
  }

  Future<void> _navigateToConfig() async {
    await const LoginCasdoorConfigRoute().push(context);
    setState(() {});
  }

  /// 显示设置文件服务器对话框
  Future<void> _showServerConfigDialog() async {
    final controller = TextEditingController(text: _ls.fileServer ?? '');
    final al = AppLocalizations.of(context);
    await showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(al.login_setServer),
        content: TextField(
          controller: controller,
          decoration: InputDecoration(
            hintText: al.login_serverHint,
            prefixIcon: Icon(Icons.link),
          ),
          autofocus: true,
          keyboardType: TextInputType.url,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: Text(al.common_cancel),
          ),
          FilledButton(
            onPressed: () async {
              final url = controller.text.trim();
              if (url.isNotEmpty) {
                await _ls.saveFileServer(url);
                if (ctx.mounted && mounted) {
                  context.read<FmRepository>().setBaseUrl(url);
                  Navigator.pop(ctx);
                  ScaffoldMessenger.of(
                    context,
                  ).showSnackBar(SnackBar(content: Text(al.login_serverSaved)));
                  // 服务器变了，重新获取认证模式
                  _refreshAuthMode();
                  setState(() {}); // 刷新界面显示新地址
                }
              }
            },
            child: Text(al.common_save),
          ),
        ],
      ),
    );
    setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;
    final al = AppLocalizations.of(context);
    final screenSize = MediaQuery.of(context).size;
    final isWideScreen = screenSize.width > 600;

    // 登录入口可用性：本地模式只需文件服务器；casdoor 模式还需 casdoor 配置
    final canLogin = _isLocalMode
        ? _ls.fileServerConfigured
        : (_ls.fileServerConfigured && _ls.casdoorConfig != null);

    return Scaffold(
      body: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxWidth: isWideScreen ? 400 : double.infinity,
            ),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // Logo / Icon
                Icon(
                  Icons.cloud_outlined,
                  size: 80,
                  color: colorScheme.primary,
                ),
                const SizedBox(height: 24),

                // Title
                Text(
                  'Personal File Manager',
                  style: theme.textTheme.headlineMedium?.copyWith(
                    fontWeight: FontWeight.bold,
                    color: colorScheme.onSurface,
                  ),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 8),

                // Subtitle
                Text(
                  'Sign in to continue',
                  style: theme.textTheme.bodyLarge?.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 48),

                if (_isLocalMode) ...[
                  // ===== 本地认证模式：账号密码表单 =====
                  TextField(
                    controller: _usernameController,
                    enabled: !_isLoading,
                    decoration: InputDecoration(
                      labelText: al.login_username,
                      prefixIcon: const Icon(Icons.person_outline),
                      border: const OutlineInputBorder(),
                    ),
                    textInputAction: TextInputAction.next,
                    autocorrect: false,
                    autofillHints: const [AutofillHints.username],
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: _passwordController,
                    enabled: !_isLoading,
                    obscureText: true,
                    decoration: InputDecoration(
                      labelText: al.login_password,
                      prefixIcon: const Icon(Icons.lock_outline),
                      border: const OutlineInputBorder(),
                    ),
                    textInputAction: TextInputAction.done,
                    autofillHints: const [AutofillHints.password],
                    onSubmitted: (_) => _handleLocalLogin(),
                  ),
                  const SizedBox(height: 24),
                  FilledButton.icon(
                    onPressed: canLogin && !_isLoading
                        ? _handleLocalLogin
                        : null,
                    icon: _isLoading
                        ? SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: colorScheme.onPrimary,
                            ),
                          )
                        : const Icon(Icons.login),
                    label: Text(
                      _isLoading ? al.login_signingIn : al.login_signIn,
                    ),
                    style: FilledButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    al.login_localHint,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                    textAlign: TextAlign.center,
                  ),
                ] else ...[
                  // ===== Casdoor 认证模式：原有登录按钮 =====
                  FilledButton.icon(
                    onPressed: canLogin && !_isLoading ? _handleLogin : null,
                    icon: _isLoading
                        ? SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: colorScheme.onPrimary,
                            ),
                          )
                        : const Icon(Icons.login),
                    label: Text(
                      _isLoading ? al.login_signingIn : al.login_signIn,
                    ),
                    style: FilledButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    al.login_casdoorHint,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: colorScheme.onSurfaceVariant,
                    ),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 16),

                  // Config Button (Outlined)
                  OutlinedButton.icon(
                    onPressed: _isLoading ? null : _navigateToConfig,
                    icon: const Icon(Icons.key),
                    label: const Text('Configure Casdoor'),
                    style: OutlinedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                  ),
                ],
                const SizedBox(height: 16),

                // Server Config Button (Tonal)
                OutlinedButton.icon(
                  onPressed: _isLoading ? null : _showServerConfigDialog,
                  icon: const Icon(Icons.dns),
                  label: Text(
                    al.login_fileServer(_ls.fileServer ?? al.login_notSet),
                  ),
                  style: OutlinedButton.styleFrom(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                  ),
                ),

                if (!_ls.fileServerConfigured) ...[
                  const SizedBox(height: 24),
                  Container(
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Row(
                      children: [
                        Icon(
                          Icons.info_outline,
                          size: 20,
                          color: colorScheme.onSurfaceVariant,
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            _isLocalMode
                                ? 'Please configure file server first'
                                : 'Please configure Casdoor service first',
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
