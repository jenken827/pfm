import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:webview_flutter/webview_flutter.dart';

import 'casdoor_auth_service.dart';
import 'local_storage.dart';
import '../models/models.dart';

class CasdoorWebViewLogin extends StatefulWidget {
  const CasdoorWebViewLogin({super.key, this.isDarkMode = false});

  final bool isDarkMode;

  @override
  State<CasdoorWebViewLogin> createState() => _CasdoorWebViewLoginState();
}

class _CasdoorWebViewLoginState extends State<CasdoorWebViewLogin> {
  late final WebViewController _controller;
  late final PkceParams _pkce;
  late final CasdoorConfig _config;
  final localStorage = GetIt.I.get<LocalStorage>();
  bool _isLoading = true;
  bool _isDarkMode = false;

  /// Loading 页面 - 检测到回调后立即显示
  String get _loadingHtml {
    final bgColor = _isDarkMode ? '#1a1a1a' : '#ffffff';
    final textColor = _isDarkMode ? '#ffffff' : '#000000';
    return '''
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Processing</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      justify-content: center;
      align-items: center;
      height: 100vh;
      margin: 0;
      background: $bgColor;
      color: $textColor;
    }
    .container {
      text-align: center;
      padding: 40px;
    }
    h1 {
      font-size: 24px;
      margin-bottom: 10px;
    }
    p {
      font-size: 16px;
      opacity: 0.7;
    }
  </style>
</head>
<body>
  <div class="container">
    <h1>Processing...</h1>
    <p>Please wait</p>
  </div>
</body>
</html>
''';
  }

  @override
  void initState() {
    super.initState();
    _isDarkMode = widget.isDarkMode;
    _pkce = CasdoorLoginHelper.generatePkce();
    _config = localStorage.casdoorConfig!;
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageStarted: (String url) {
            setState(() => _isLoading = true);
            _checkForCallback(url);
          },
          onPageFinished: (String url) {
            setState(() => _isLoading = false);
          },
          onNavigationRequest: (NavigationRequest request) {
            return NavigationDecision.navigate;
          },
        ),
      );
    _loadLoginPage();
  }

  void _loadLoginPage() {
    final authUrl = CasdoorAuthUrlBuilder(
      serverUrl: _config.serverUrl,
      clientId: _config.clientId,
      redirectUri: CasdoorLoginHelper.redirectUri,
      codeChallenge: _pkce.codeChallenge,
      state: _pkce.state,
    ).build().toString();

    _controller.loadRequest(Uri.parse(authUrl));
  }

  bool _isProcessing = false;

  Future<void> _checkForCallback(String url) async {
    // 检查是否到达回调页面
    if (url.contains('code=') && !_isProcessing) {
      _isProcessing = true;

      // 立即显示 loading 页面，阻止 Casdoor 回调页面显示
      await _controller.loadHtmlString(_loadingHtml);

      final uri = Uri.parse(url);
      final code = uri.queryParameters['code'];
      final callbackState = uri.queryParameters['state'];

      if (code != null && callbackState == _pkce.state) {
        // 交换 token
        try {
          final tokenExchange = CasdoorTokenExchange(
            serverUrl: _config.serverUrl,
            clientId: _config.clientId,
          );
          final token = await tokenExchange.exchange(code, _pkce.codeVerifier);
          final user = await CasdoorLoginHelper.saveLoginInfo(token);
          if (mounted) {
            Navigator.of(context).pop(user);
          }
        } catch (e) {
          if (mounted) {
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(SnackBar(content: Text('Login failed: $e')));
            Navigator.of(context).pop();
          }
        }
      } else if (callbackState != _pkce.state) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('State mismatch')));
          Navigator.of(context).pop();
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Login'),
        leading: IconButton(
          icon: const Icon(Icons.close),
          onPressed: () => Navigator.of(context).pop(),
        ),
      ),
      body: Stack(
        children: [
          WebViewWidget(controller: _controller),
          if (_isLoading || _isProcessing)
            const Center(child: CircularProgressIndicator()),
        ],
      ),
    );
  }
}

/// 启动 Casdoor WebView 登录，返回登录用户
Future<User?> showCasdoorWebViewLogin(BuildContext context) async {
  final brightness = Theme.of(context).brightness;
  final isDarkMode = brightness == Brightness.dark;
  return Navigator.of(context).push<User>(
    MaterialPageRoute(
      builder: (context) => CasdoorWebViewLogin(isDarkMode: isDarkMode),
    ),
  );
}
