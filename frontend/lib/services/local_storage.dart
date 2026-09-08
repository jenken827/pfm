import 'dart:convert';
import 'dart:math';
import 'package:flex_color_scheme/flex_color_scheme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:hive_ce_flutter/adapters.dart';
import 'package:pfm/proto/native/file.pb.dart' show PhotoDateConfig;
import 'package:pfm/services/album_backup_service.dart';
import 'package:pfm/services/logger.dart';
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
  // 用户登录信息键
  static const String _userKey = 'user';
  // 登录令牌键
  static const String _tokenKey = 'token';
  // 服务器 URL 键
  static const String _fileServerKey = 'serverurl';
  // 传输服务 URL 键
  static const String _transferUrlKey = 'transfer_url';
  // 照片日期解析配置缓存键（相册备份年/月归档规则，服务端下发的 JSON）
  static const String _photoDateConfigKey = 'photo_date_config';

  late final Box _box;
  late final Box _settingsBox;

  String _currentThemeName = '';

  String get currentThemeName => _currentThemeName;

  Box get settingsBox => _settingsBox;

  // 检查文件服务器是否已配置（登录入口可用）
  bool get fileServerConfigured =>
      fileServer != null && fileServer!.isNotEmpty;

  // 兼容旧调用：文件服务器已配置即可视为"已配置"
  bool get isConfigured => fileServerConfigured;

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

    _box = await _openBoxResilient(
      _boxname,
      encryptionCipher: HiveAesCipher(encryptionKeyUint8List),
    );
    _settingsBox = await _openBoxResilient(_settingsBoxname);
  }

  /// 打开 Hive box，兼容历史版本遗留数据导致的 typeId 漂移：
  /// 旧版本（GenerateAdapters 迁移前/适配器集合变更期间）写入的帧 typeId 可能
  /// 已不在当前适配器表（如 unknown typeId: 34），openBox 读帧时会直接抛
  /// HiveError 导致应用启动崩溃。这里遇到该错误时备份并重置 box（本地登录态/
  /// 服务器地址等一次性丢失，重新登录即可），避免启动即崩。
  Future<Box> _openBoxResilient(
    String name, {
    HiveAesCipher? encryptionCipher,
  }) async {
    try {
      return await Hive.openBox(name, encryptionCipher: encryptionCipher);
    } on HiveError catch (e) {
      if (!e.message.contains('unknown typeId')) rethrow;
      getLogger().error(
        'Hive box $name 含未注册 typeId 的旧数据（${e.message}），'
        '重置该 box（本地登录态/服务器配置将清除，需重新登录）',
      );
      await Hive.deleteBoxFromDisk(name);
      return Hive.openBox(name, encryptionCipher: encryptionCipher);
    }
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

  /// 获取已登录的用户
  User? get user => _box.get(_userKey);

  /// 保存用户
  Future<void> saveUser(User user) async {
    await _box.put(_userKey, user);
  }

  /// 获取登录令牌
  AuthToken? get token => _box.get(_tokenKey);

  /// 保存登录令牌
  Future<void> saveToken(AuthToken token) async {
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

  /// 获取传输服务 URL（高带宽上传下载地址）
  String? get transferUrl {
    final url = _box.get(_transferUrlKey);
    return (url != null && url.isNotEmpty) ? url : null;
  }

  /// 保存传输服务 URL
  Future<void> saveTransferUrl(String url) async {
    await _box.put(_transferUrlKey, url);
  }

  /// 获取下载/预览用的 base URL（优先传输服务地址）
  String get downloadBaseUrl => transferUrl ?? fileServer ?? '';

  /// 获取缓存的照片日期解析配置（服务端下发；无缓存返回 null 由调用方回退内置默认）
  String? get photoDateConfigJson => _box.get(_photoDateConfigKey) as String?;

  /// 保存照片日期解析配置（序列化为 JSON 字符串缓存）
  Future<void> savePhotoDateConfigJson(PhotoDateConfig config) async {
    await _box.put(_photoDateConfigKey, config.writeToJson());
  }

  /// 获取相册备份配置
  AlbumBackupConfig? get albumBackupConfig {
    final configJson = _settingsBox.get('albumBackupConfig');
    if (configJson != null) {
      return AlbumBackupConfig.fromJson(Map<String, dynamic>.from(configJson));
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
    return _settingsBox.get(
      'smallFileThreshold',
      defaultValue: 5 * 1024 * 1024,
    );
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

  /// 日志等级（talker LogLevel.name 的字符串，如 'debug'/'info'），
  /// 未配置时返回 null，由 logger 按构建模式取默认值
  String? get logLevel {
    return _settingsBox.get('logLevel') as String?;
  }

  /// 保存日志等级
  Future<void> saveLogLevel(String level) async {
    await _settingsBox.put('logLevel', level);
  }
}
