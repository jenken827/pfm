/// 登录令牌（本地认证 JWT 的存取容器）。
/// Hive typeId/字段布局与历史版本保持一致，升级后已存令牌仍可读取。
class AuthToken {
  String? accessToken;
  String? tokenType;
  int? expiresIn;
  String? refreshToken;
  String? scope;

  AuthToken();

  AuthToken.fromMap(Map<String, dynamic> map)
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
            (map['roles'] as List<dynamic>? ?? const [])
                .map((e) => e.toString())
                .toList(),
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
}
