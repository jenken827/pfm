import 'dart:convert';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:get_it/get_it.dart';
import 'package:http/http.dart' as http;
import 'package:jwt_decoder/jwt_decoder.dart';
import 'package:path/path.dart';

import 'local_storage.dart';
import '../models/models.dart';

/// Casdoor PKCE 工具类
class CasdoorPkce {
  /// 生成随机字符串
  static String generateRandomString(int length) {
    final random = Random();
    const chars =
        'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    return List.generate(
      length,
      (_) => chars[random.nextInt(chars.length)],
    ).join();
  }

  /// 生成 PKCE 代码挑战
  static String generateCodeChallenge(String verifier) {
    final bytes = utf8.encode(verifier);
    final digest = sha256.convert(bytes);
    return base64Url.encode(digest.bytes).replaceAll('=', '');
  }
}

/// Casdoor 授权 URL 构建器
class CasdoorAuthUrlBuilder {
  final String serverUrl;
  final String clientId;
  final String redirectUri;
  final String codeChallenge;
  final String state;

  CasdoorAuthUrlBuilder({
    required this.serverUrl,
    required this.clientId,
    required this.redirectUri,
    required this.codeChallenge,
    required this.state,
  });

  /// 构建授权 URL
  Uri build() {
    final url = Uri.parse(serverUrl);
    return Uri(
      scheme: url.scheme,
      host: url.host,
      path: '/login/oauth/authorize',
      queryParameters: {
        'client_id': clientId,
        'response_type': 'code',
        'scope': 'read',
        'state': state,
        'code_challenge_method': 'S256',
        'code_challenge': codeChallenge,
        'redirect_uri': redirectUri,
      },
    );
  }
}

/// Casdoor Token 交换服务
class CasdoorTokenExchange {
  final String serverUrl;
  final String clientId;

  const CasdoorTokenExchange({required this.serverUrl, required this.clientId});

  /// 交换 code 获取 access token
  Future<CasdoorToken> exchange(String code, String codeVerifier) async {
    final url = join(serverUrl, 'api/login/oauth/access_token');
    final response = await http.post(
      Uri.parse(url),
      headers: {'Content-Type': 'application/x-www-form-urlencoded'},
      body: {
        'client_id': clientId,
        'grant_type': 'authorization_code',
        'code': code,
        'code_verifier': codeVerifier,
      },
    );

    if (response.statusCode == 200) {
      final data = jsonDecode(response.body);
      return CasdoorToken.fromMap(data);
    } else {
      throw Exception('Failed to exchange token: ${response.body}');
    }
  }
}

/// Casdoor 登录辅助类
class CasdoorLoginHelper {
  static const int listenPort = 58899;
  static const String redirectUri = "http://localhost:$listenPort/callback";

  /// 从 token 解析用户信息并保存
  static Future<User> saveLoginInfo(CasdoorToken token) async {
    final userInfo = JwtDecoder.decode(token.accessToken!);
    final user = User.fromMap(userInfo);

    final ls = GetIt.I.get<LocalStorage>();
    await ls.saveToken(token);
    await ls.saveUser(user);

    return user;
  }

  /// 检查 Casdoor 是否已配置
  static bool isConfigured() {
    final config = GetIt.I.get<LocalStorage>().casdoorConfig;
    return config != null &&
        config.serverUrl.isNotEmpty &&
        config.clientId.isNotEmpty;
  }

  /// 获取 Casdoor 配置
  static CasdoorConfig? getConfig() {
    return GetIt.I.get<LocalStorage>().casdoorConfig;
  }

  /// 生成 PKCE 参数
  static PkceParams generatePkce() {
    final codeVerifier = CasdoorPkce.generateRandomString(43);
    final codeChallenge = CasdoorPkce.generateCodeChallenge(codeVerifier);
    final state = CasdoorPkce.generateRandomString(16);
    return PkceParams(
      codeVerifier: codeVerifier,
      codeChallenge: codeChallenge,
      state: state,
    );
  }
}

class PkceParams {
  final String codeVerifier;

  final String codeChallenge;

  final String state;

  const PkceParams({
    required this.codeVerifier,

    required this.codeChallenge,

    required this.state,
  });
}
