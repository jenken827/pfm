import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';

import '../../models/models.dart';
import '../../router/router.dart';
import '../../services/services.dart';

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
    final ls = GetIt.I.get<LocalStorage>();
    setState(() {
      _user = ls.user;
    });
  }

  /// 清除本地登录态并回到登录页（重新登录）
  Future<void> _handleRelogin() async {
    final ls = GetIt.I.get<LocalStorage>();
    await ls.clearLoginInfo();
    if (mounted) {
      _loadUser();
      const LoginRoute().go(context);
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
              onPressed: _handleRelogin,
              child: Text(_user != null ? 'Re-login' : 'Login'),
            ),
          ],
        ),
      ),
    );
  }
}
