import 'dart:async';
import 'dart:io';

import 'package:get_it/get_it.dart';
import 'package:pfm/services/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models/models.dart';

/// OAuth 登录被取消/超时（例如用户直接关闭了浏览器登录页）
class OAuthLoginCancelledException implements Exception {
  final String message;

  const OAuthLoginCancelledException(this.message);

  @override
  String toString() => message;
}

class DesktopOAuthService {
  DesktopOAuthService() {
    _pkce = CasdoorLoginHelper.generatePkce();
    _config = _localStorage.casdoorConfig!;
  }

  /// 回调等待超时：用户关闭浏览器登录页或长时间未完成登录时自动取消，
  /// 避免 UI 永久卡在"登录中"状态、无法进行任何操作
  static const Duration callbackTimeout = Duration(minutes: 2);

  late final PkceParams _pkce;
  late final CasdoorConfig _config;
  final _localStorage = GetIt.I.get<LocalStorage>();
  HttpServer? _server;

  /// 启动本地服务器监听回调
  Future<String> _startCallbackServer(String state) async {
    final completer = Completer<String>();

    try {
      _server = await HttpServer.bind(
        InternetAddress.loopbackIPv4,
        CasdoorLoginHelper.listenPort,
      );
    } catch (e) {
      // 端口被占用（上次登录异常未关闭等），无法监听回调
      throw Exception('Failed to start callback server: $e');
    }

    _server!.listen((request) async {
      if (request.uri.path == '/callback') {
        final code = request.uri.queryParameters['code'];
        final callbackState = request.uri.queryParameters['state'];
        final error = request.uri.queryParameters['error'];

        // 用户拒绝授权/取消：立即结束登录流程（防卡死）
        if (error != null) {
          if (!completer.isCompleted) {
            completer.completeError(
              const OAuthLoginCancelledException('Login cancelled'),
            );
          }
        } else if (callbackState != state) {
          if (!completer.isCompleted) {
            completer.completeError(Exception('State mismatch'));
          }
        } else if (code != null) {
          if (!completer.isCompleted) {
            completer.complete(code);
          }
        } else {
          if (!completer.isCompleted) {
            completer.completeError(Exception('No code received'));
          }
        }

        // 发送成功页面（server 的关闭由 login() 的 finally 统一处理，
        // 保证超时/异常路径也不会残留占用回调端口）
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
      }
    });

    return completer.future;
  }

  /// 执行 OAuth 登录流程，返回登录用户
  ///
  /// 用户直接关闭浏览器登录页或超时未完成登录时返回 null（不抛异常），
  /// 调用方保持登录页可操作状态，可随时重新点击登录。
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

    try {
      // 等待回调（带超时：用户关闭浏览器页面后不会永久卡死）
      final code = await _startCallbackServer(_pkce.state).timeout(
        callbackTimeout,
        onTimeout: () {
          getLogger().warning(
            'OAuth callback timed out after $callbackTimeout',
          );
          throw const OAuthLoginCancelledException(
            'Login timed out, please retry',
          );
        },
      );
      getLogger().info(
        'login callback code $code,serverUrl: ${_config.serverUrl}',
      );

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
    } on OAuthLoginCancelledException {
      // 用户取消/超时：静默返回，UI 恢复可操作状态
      getLogger().info('OAuth login cancelled by user');
      return null;
    } catch (e) {
      getLogger().error('exchange token failed, message: $e');
      rethrow; // 其他错误（端口占用、state 不匹配等）由调用方提示
    } finally {
      // 确保回调服务器被关闭，下次登录可重新绑定端口
      await _server?.close();
      _server = null;
    }
  }

  void dispose() {
    _server?.close();
    _server = null;
  }
}
