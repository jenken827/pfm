import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';

import '../../models/casdoor.dart';
import '../../router/router.dart';
import '../../services/casdoor_webview_login.dart';
import '../../services/desktop_oauth_service.dart';
import '../../services/local_storage.dart';
import '../../utils/utils.dart';

class HomePage extends StatefulWidget {
  const HomePage({super.key});

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  User? _user;

  @override
  void initState() {
    super.initState();
    _loadUser();
  }

  void _loadUser() {
    final ls = GetIt.I<LocalStorage>();
    setState(() {
      _user = ls.user;
    });
  }

  Future<void> _handleLogin() async {
    try {
      User? user;

      if (isDesktop()) {
        final oauthService = DesktopOAuthService();
        user = await oauthService.login();
        oauthService.dispose();
      } else if (isMobile()) {
        user = await showCasdoorWebViewLogin(context);
      }

      if (user != null && mounted) {
        _loadUser();
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Login success: ${user.name}')));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('Login error: $e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home Page')),
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text('Welcome to the Home Page!'),
            const SizedBox(height: 20),
            FilledButton(
              onPressed: () {
                const HomeChildRoute().push(context);
              },
              child: const Text('Go to Home Child Page'),
            ),
            const SizedBox(height: 20),
            if (_user != null) ...[
              Text('Logged in as: ${_user!.name}'),
              const SizedBox(height: 20),
            ],
            FilledButton(
              onPressed: _handleLogin,
              child: Text(_user != null ? 'Re-login' : 'Login'),
            ),
          ],
        ),
      ),
    );
  }
}
