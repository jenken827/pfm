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
  final _ls = GetIt.I.get<LocalStorage>();

  Future<void> _handleLogin() async {
    if (!_ls.isConfigured || _isLoading) return;

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
    final al = AppLocalizations.of(context)!;
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
    final al = AppLocalizations.of(context)!;
    final screenSize = MediaQuery.of(context).size;
    final isWideScreen = screenSize.width > 600;

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

                // Login Button
                FilledButton.icon(
                  onPressed: _ls.isConfigured && !_isLoading
                      ? _handleLogin
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
                  label: Text(_isLoading ? 'Signing in...' : 'Login'),
                  style: FilledButton.styleFrom(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                  ),
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
                const SizedBox(height: 16),

                // Server Config Button (Tonal)
                OutlinedButton.icon(
                  onPressed: _isLoading ? null : _showServerConfigDialog,
                  icon: const Icon(Icons.dns),
                  label: Text(al.login_fileServer(_ls.fileServer ?? al.login_notSet)),
                  style: OutlinedButton.styleFrom(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                  ),
                ),

                if (!_ls.isConfigured) ...[
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
                            'Please configure Casdoor service first',
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
