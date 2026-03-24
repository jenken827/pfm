import 'dart:async';
import 'dart:io';

import 'package:get_it/get_it.dart';
import 'package:pfm/services/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models/models.dart';

class DesktopOAuthService {
  DesktopOAuthService() {
    _pkce = CasdoorLoginHelper.generatePkce();
    _config = _localStorage.casdoorConfig!;
  }

  late final PkceParams _pkce;
  late final CasdoorConfig _config;
  final _localStorage = GetIt.I.get<LocalStorage>();
  HttpServer? _server;

  /// 启动本地服务器监听回调
  Future<String> _startCallbackServer(String state) async {
    final completer = Completer<String>();

    _server = await HttpServer.bind(
      InternetAddress.loopbackIPv4,
      CasdoorLoginHelper.listenPort,
    );

    _server!.listen((request) async {
      if (request.uri.path == '/callback') {
        final code = request.uri.queryParameters['code'];
        final callbackState = request.uri.queryParameters['state'];
        if (callbackState != state) {
          completer.completeError(Exception('State mismatch'));
        }
        if (code != null) {
          completer.complete(code);
        } else {
          completer.completeError(Exception('No code received'));
        }

        // 发送成功页面后关闭
        request.response.statusCode = 200;
        request.response.headers.set('Content-Type', 'text/html');
        request.response.write('''
          <!DOCTYPE html>
          <html>
          <head><title>Success</title></head>
          <body>
            <h1>Login Successful!</h1>
            <p>You can close this window and return to the application.</p>
            <script>setTimeout(() => window.close(), 2000);</script>
          </body>
          </html>
        ''');
        await request.response.close();
        await _server?.close();
      }
    });

    return completer.future;
  }

  /// 执行 OAuth 登录流程，返回登录用户
  Future<User?> login() async {
    // 构建授权 URL
    final authUri = CasdoorAuthUrlBuilder(
      serverUrl: _config.serverUrl,
      clientId: _config.clientId,
      redirectUri: CasdoorLoginHelper.redirectUri,
      codeChallenge: _pkce.codeChallenge,
      state: _pkce.state,
    ).build();

    // 打开系统浏览器
    if (!await launchUrl(authUri)) {
      throw Exception('Failed to open browser');
    }

    // 等待回调
    final code = await _startCallbackServer(_pkce.state);
    getLogger().info('login callback code $code');

    try {
      // 交换 access token
      final tokenExchange = CasdoorTokenExchange(
        serverUrl: _config.serverUrl,
        clientId: _config.clientId,
      );
      final token = await tokenExchange.exchange(code, _pkce.codeVerifier);
      getLogger().info('token exchanged :$token');
      if (token.accessToken == null) {
        return null;
      }
      // 保存登录信息并返回用户
      return await CasdoorLoginHelper.saveLoginInfo(token);
    } catch (e) {
      getLogger().error('exchange token failed, message: $e');
      return null;
    }
  }

  void dispose() {
    _server?.close();
  }
}
