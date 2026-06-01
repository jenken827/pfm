import 'dart:convert';
import 'dart:math';
import 'package:flex_color_scheme/flex_color_scheme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:hive_ce_flutter/adapters.dart';
import 'package:pfm/services/album_backup_service.dart';
import '../models/models.dart';

class LocalStorage {
  LocalStorage._();

  static Future<LocalStorage> instance() async {
    final inst = LocalStorage._();
    await inst._init();
    return inst;
  }

  // 存储加密数据
  static const String _boxname = 'box.db';
  // 存储设置数据
  static const String _settingsBoxname = 'settings.db';

  // 存储本地加密密钥的键
  static const String _localEncryptionKeyKey = "localencryptkey";
  // 主题语言配置键
  static const String _themeLanguageKey = 'themelanguage';
  // Casdoor 配置键
  static const String _casdoorConfigKey = 'casdoorconfig';
  // 用户登录信息键
  static const String _userKey = 'user';
  // Casdoor Token 键
  static const String _tokenKey = 'token';
  // 服务器 URL 键
  static const String _fileServerKey = 'serverurl';

  late final Box _box;
  late final Box _settingsBox;

  String _currentThemeName = '';

  String get currentThemeName => _currentThemeName;

  Box get settingsBox => _settingsBox;

  // 检查casdoor服务和文件服务是否配置完成
  bool get isConfigured =>
      casdoorConfig != null && fileServer != null && fileServer!.isNotEmpty;

  /// 检查用户是否已登录
  bool get isLoggedIn => user != null && token != null;

  Future<void> _init() async {
    const secureStorage = FlutterSecureStorage();
    final localEncryptionKey = await secureStorage.read(
      key: _localEncryptionKeyKey,
    );

    if (localEncryptionKey == null) {
      final key = Hive.generateSecureKey();
      await secureStorage.write(
        key: _localEncryptionKeyKey,
        value: base64UrlEncode(key),
      );
    }
    final key = await secureStorage.read(key: _localEncryptionKeyKey);

    final encryptionKeyUint8List = base64Url.decode(key!);

    _box = await Hive.openBox(
      _boxname,
      encryptionCipher: HiveAesCipher(encryptionKeyUint8List),
    );
    _settingsBox = await Hive.openBox(_settingsBoxname);
  }

  ThemeLanguage get themeLanguage {
    final tl = _settingsBox.get(_themeLanguageKey);
    if (tl != null) {
      if (_currentThemeName.isNotEmpty) {
        return tl;
      }
      if (tl.themeName == 'random') {
        _currentThemeName =
            FlexScheme.values[Random().nextInt(FlexScheme.values.length)].name;
      } else {
        _currentThemeName = tl.themeName;
      }

      return tl;
    }
    //没有记录配置，初始化
    if (_currentThemeName.isEmpty) {
      _currentThemeName = FlexScheme.values[0].name;
    }
    return ThemeLanguage(
      themeMode: ThemeMode.light,
      themeName: _currentThemeName,
      locale: 'zh',
    );
  }

  Future<void> saveThemeLanguage(ThemeLanguage tl) async {
    if (tl.themeName != 'random') {
      _currentThemeName = tl.themeName;
    }
    await _settingsBox.put(_themeLanguageKey, tl);
  }

  /// 获取 Casdoor 配置
  CasdoorConfig? get casdoorConfig => _box.get(_casdoorConfigKey);

  /// 保存 Casdoor 配置
  Future<void> saveCasdoorConfig(CasdoorConfig config) async {
    await _box.put(_casdoorConfigKey, config);
  }

  /// 获取已登录的用户
  User? get user => _box.get(_userKey);

  /// 保存用户
  Future<void> saveUser(User user) async {
    await _box.put(_userKey, user);
  }

  /// 获取 Casdoor Token
  CasdoorToken? get token => _box.get(_tokenKey);

  /// 保存 Token
  Future<void> saveToken(CasdoorToken token) async {
    await _box.put(_tokenKey, token);
  }

  /// 获取服务器 URL
  String? get fileServer {
    final url = _box.get(_fileServerKey);
    return url;
  }

  /// 保存服务器 URL
  Future<void> saveFileServer(String url) async {
    await _box.put(_fileServerKey, url);
  }

  /// 获取相册备份配置
  AlbumBackupConfig? get albumBackupConfig {
    final configJson = _settingsBox.get('albumBackupConfig');
    if (configJson != null) {
      return AlbumBackupConfig.fromJson(
        Map<String, dynamic>.from(configJson),
      );
    }
    return null;
  }

  /// 保存相册备份配置
  Future<void> saveAlbumBackupConfig(AlbumBackupConfig config) async {
    await _settingsBox.put('albumBackupConfig', config.toJson());
  }

  /// 清除登录信息（退出登录）
  Future<void> clearLoginInfo() async {
    await _box.delete(_userKey);
    await _box.delete(_tokenKey);
  }

  /// 获取通讯录备份配置
  ContactBackupConfig? get contactBackupConfig {
    final configJson = _settingsBox.get('contactBackupConfig');
    if (configJson != null) {
      return ContactBackupConfig.fromJson(
        Map<String, dynamic>.from(configJson),
      );
    }
    return null;
  }

  /// 保存通讯录备份配置
  Future<void> saveContactBackupConfig(ContactBackupConfig config) async {
    await _settingsBox.put('contactBackupConfig', config.toJson());
  }

  /// 上传分片大小（字节），默认 2MB
  int get uploadChunkSize {
    return _settingsBox.get('uploadChunkSize', defaultValue: 2 * 1024 * 1024);
  }

  /// 保存上传分片大小
  Future<void> saveUploadChunkSize(int size) async {
    await _settingsBox.put('uploadChunkSize', size);
  }

  /// 小文件上传阈值（字节），默认 5MB
  int get smallFileThreshold {
    return _settingsBox.get('smallFileThreshold', defaultValue: 5 * 1024 * 1024);
  }

  /// 保存小文件上传阈值
  Future<void> saveSmallFileThreshold(int size) async {
    await _settingsBox.put('smallFileThreshold', size);
  }

  /// 上传并发数，默认 3
  int get uploadConcurrency {
    return _settingsBox.get('uploadConcurrency', defaultValue: 3);
  }

  /// 保存上传并发数
  Future<void> saveUploadConcurrency(int count) async {
    await _settingsBox.put('uploadConcurrency', count);
  }

  /// 下载并发数，默认 3
  int get downloadConcurrency {
    return _settingsBox.get('downloadConcurrency', defaultValue: 3);
  }

  /// 保存下载并发数
  Future<void> saveDownloadConcurrency(int count) async {
    await _settingsBox.put('downloadConcurrency', count);
  }
}
