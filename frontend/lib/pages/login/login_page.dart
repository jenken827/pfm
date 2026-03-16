import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import '../../models/models.dart';
import '../../router/router.dart';
import '../../services/services.dart';
import '../../utils/utils.dart';

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
    final controller = TextEditingController(
      text: _ls.fileServer ?? 'http://localhost:8080',
    );
    await showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('设置文件服务器'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: '请输入服务器地址',
            prefixIcon: Icon(Icons.link),
          ),
          autofocus: true,
          keyboardType: TextInputType.url,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () async {
              final url = controller.text.trim();
              if (url.isNotEmpty) {
                await _ls.saveFileServer(url);
                if (ctx.mounted && mounted) {
                  Navigator.pop(ctx);
                  ScaffoldMessenger.of(
                    context,
                  ).showSnackBar(const SnackBar(content: Text('服务器地址已保存')));
                  setState(() {}); // 刷新界面显示新地址
                }
              }
            },
            child: const Text('保存'),
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
                  label: Text('File Server: ${_ls.fileServer ?? "未设置"}'),
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
