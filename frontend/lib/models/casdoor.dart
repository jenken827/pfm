class CasdoorConfig {
  final String clientId;
  final String serverUrl;
  final String organizationName;
  final String appName;

  const CasdoorConfig({
    required this.clientId,
    required this.serverUrl,
    required this.organizationName,
    required this.appName,
  });
}

class CasdoorToken {
  String? accessToken;
  String? tokenType;
  int? expiresIn;
  String? refreshToken;
  String? scope;

  CasdoorToken();

  CasdoorToken.fromMap(Map<String, dynamic> map)
    : accessToken = map['access_token'],
      tokenType = map['token_type'],
      expiresIn = map['expires_in'],
      refreshToken = map['refresh_token'],
      scope = map['scope'];

  Map<String, dynamic> toJson() => {
    'access_token': accessToken,
    'token_type': tokenType,
    'expires_in': expiresIn,
    'refresh_token': refreshToken,
    'scope': scope,
  };
}

class User {
  final String id;
  final String name;
  final DateTime? createdTime;
  final DateTime? updatedTime;
  final String? avatar;
  final String? email;
  final String? phone;
  final List<String> roles;

  User({
    required this.id,
    required this.name,
    this.createdTime,
    this.updatedTime,
    this.avatar,
    this.email,
    this.phone,
    this.roles = const [],
  });
  User.fromMap(Map<String, dynamic> map)
    : this(
        id: map['id'],
        name: map['name'],
        createdTime: map['createdTime'] != null
            ? DateTime.parse(map['createdTime'])
            : null,
        updatedTime: map['updatedTime'] != null
            ? DateTime.parse(map['updatedTime'])
            : null,
        avatar: map['avatar'],
        email: map['email'],
        phone: map['phone'],
        roles:
            (map['roles'] as List<dynamic>?)
                ?.map((e) => e.toString())
                .toList() ??
            [],
      );
}

/// 本地认证登录结果（POST /api/auth/login）
class LocalLoginResult {
  final String token;
  final String userId;
  final String name;
  final String email;
  final bool isAdmin;

  const LocalLoginResult({
    required this.token,
    required this.userId,
    required this.name,
    required this.email,
    required this.isAdmin,
  });

  factory LocalLoginResult.fromMap(Map<String, dynamic> map) {
    final user = map['user'] as Map<String, dynamic>? ?? const {};
    return LocalLoginResult(
      token: map['token']?.toString() ?? '',
      userId: user['id']?.toString() ?? '',
      name: user['name']?.toString() ?? '',
      email: user['email']?.toString() ?? '',
      isAdmin: user['is_admin'] == true,
    );
  }
}

/// 认证模式（与后端 /api/auth/config 的 mode 字段对应）
class AuthMode {
  static const String casdoor = 'casdoor';
  static const String local = 'local';
}
