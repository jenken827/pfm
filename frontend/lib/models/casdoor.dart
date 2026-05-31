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

  User({
    required this.id,
    required this.name,
    this.createdTime,
    this.updatedTime,
    this.avatar,
    this.email,
    this.phone,
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
      );
}
