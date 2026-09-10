import 'dart:async';

import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:fixnum/fixnum.dart';
import 'package:pfm/models/models.dart';
import 'package:protobuf/well_known_types/google/protobuf/empty.pb.dart';
import 'package:talker_flutter/talker_flutter.dart';

import '../proto/native/auth.pb.dart';
import '../proto/native/backup.pb.dart';
import '../proto/native/download.pb.dart';
import '../proto/native/enum.pbenum.dart';
import '../proto/native/file.pb.dart';
import '../proto/native/permission.pb.dart';
import '../proto/native/provider.pb.dart';
import '../proto/native/upload.pb.dart';
import '../router/router.dart';
import '../services/local_storage.dart';
import '../services/background_service.dart';
import '../services/logger.dart';
import 'util.dart';

/// API 响应结果
class ApiResult<T> {
  final T? data;
  final String? message;
  final bool success;

  ApiResult({this.data, this.message, required this.success});
}

/// 文件管理器 Repository - 负责与后端 API 对接
class FmRepository {
  late final Dio _dio;
  late final Dio _uploadDio; // 上传专用 Dio，超时时间更长
  Talker get _logger => getLogger();

  String? _baseUrl;
  String? _transferUrl; // 传输服务地址（高带宽上传下载）

  /// 上传/下载用的 base URL，优先使用传输服务地址
  String get _uploadBaseUrl => _transferUrl ?? _baseUrl ?? '';

  FmRepository._();

  factory FmRepository() {
    final inst = FmRepository._();
    final ls = GetIt.I.get<LocalStorage>();
    inst._init(ls.fileServer);
    return inst;
  }

  /// 处理 Token 失效
  Future<void> _handleTokenInvalid() async {
    _logger.error(
      'Token invalid, clearing login info and redirecting to login',
    );
    final ls = GetIt.I.get<LocalStorage>();
    await ls.clearLoginInfo();
    // 停止所有后台传输任务
    GetIt.I.get<BackgroundService>().cancelAllTransfers();
    approuter.go('/login');
  }

  void _init(String? baseUrl) {
    _baseUrl = baseUrl;
    _dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 30),
        receiveTimeout: const Duration(seconds: 30),
        sendTimeout: const Duration(seconds: 30),
        // 默认使用 JSON 格式
        contentType: 'application/json',
      ),
    );

    // 上传专用 Dio：sendTimeout 更长，避免批量上传时超时
    _uploadDio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 30),
        receiveTimeout: const Duration(minutes: 10),
        sendTimeout: const Duration(minutes: 10),
        contentType: 'application/json',
      ),
    );

    // 添加拦截器
    final authInterceptor = InterceptorsWrapper(
      onRequest: (options, handler) {
        // 动态获取认证 Token，确保登录后能使用最新 token
        final ls = GetIt.I.get<LocalStorage>();
        final token = ls.token;
        if (token?.accessToken != null) {
          options.headers['Authorization'] = 'Bearer ${token!.accessToken}';
        }
        _logger.debug('API Request: ${options.method} ${options.path}');
        return handler.next(options);
      },
      onResponse: (response, handler) {
        _logger.debug(
          'API Response: ${response.statusCode} ${response.requestOptions.path}',
        );
        return handler.next(response);
      },
      onError: (error, handler) {
        _logger.error(
          'API Error: ${error.message} ${error.requestOptions.path}\n'
          'Status: ${error.response?.statusCode}\n'
          'Response: ${error.response?.data}',
        );
        return handler.next(error);
      },
    );
    _dio.interceptors.add(authInterceptor);
    _uploadDio.interceptors.add(authInterceptor);
  }

  /// 获取传输服务配置并缓存
  Future<void> fetchTransferConfig() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/file/transfer-config',
        data: GetTransferConfigReq().toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetTransferConfigResp>(
        data,
        GetTransferConfigResp(),
      );
      if (respData.serverUrl.isNotEmpty) {
        _transferUrl = respData.serverUrl;
        final ls = GetIt.I.get<LocalStorage>();
        await ls.saveTransferUrl(respData.serverUrl);
        _logger.info('传输服务地址: $_transferUrl');
      }
    } catch (e) {
      _logger.warning('获取传输服务配置失败（非致命）: $e');
    }
  }

  /// 获取照片日期解析配置（相册备份按年/月归档用）
  /// 成功则缓存到本地，后续离线相册备份回退本地缓存，仍无则用内置默认规则
  Future<ApiResult<PhotoDateConfig>> fetchPhotoDateConfig() async {
    try {
      final req = GetPhotoDateConfigReq();

      final response = await _dio.post(
        '$_baseUrl/api/file/photo-date-config',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<PhotoDateConfig>(data, PhotoDateConfig());
      // 缓存 JSON 字符串，相册备份无网络时仍可用最近一次的服务端规则
      final ls = GetIt.I.get<LocalStorage>();
      await ls.savePhotoDateConfigJson(respData);
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.warning('获取照片日期配置失败（非致命）: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.warning('获取照片日期配置失败（非致命）: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 本地认证登录（账号密码 → 服务端签发 JWT）
  /// 成功后调用方负责保存 token 与用户信息
  /// 请求/响应均走 proto 契约（LoginReq / LoginResp），使用 Proto3Json 编解码。
  Future<ApiResult<LocalLoginResult>> loginWithPassword({
    required String username,
    required String password,
  }) async {
    try {
      final req = LoginReq()
        ..username = username
        ..password = password;
      final response = await _dio.post(
        '$_baseUrl/api/auth/login',
        data: req.toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      final resp = LoginResp();
      resp.mergeFromProto3Json(data);
      final result = LocalLoginResult(
        token: resp.token,
        userId: resp.userId,
        name: resp.name,
        email: resp.email,
        isAdmin: resp.isAdmin,
      );
      _logger.info('本地登录成功: user=${result.name}');
      return ApiResult(success: true, data: result);
    } on DioException catch (e) {
      _logger.error('本地登录失败: ${e.message}');
      // 401 = 用户名或密码错误（后端返回明文错误信息）
      if (e.response?.statusCode == 401) {
        final body = e.response?.data;
        return ApiResult(
          success: false,
          message: body?.toString() ?? '用户名或密码错误',
        );
      }
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('本地登录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 用于 isolate 中初始化，需要手动传入 token
  void _initWithToken(String? baseUrl, AuthToken? token) {
    _baseUrl = baseUrl;
    _dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 30),
        receiveTimeout: const Duration(seconds: 30),
        sendTimeout: const Duration(seconds: 30),
        contentType: 'application/json',
      ),
    );

    // 上传专用 Dio：sendTimeout 更长，避免批量上传时超时
    _uploadDio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 30),
        receiveTimeout: const Duration(minutes: 10),
        sendTimeout: const Duration(minutes: 10),
        contentType: 'application/json',
      ),
    );

    final authInterceptor = InterceptorsWrapper(
      onRequest: (options, handler) {
        if (token?.accessToken != null) {
          options.headers['Authorization'] = 'Bearer ${token!.accessToken}';
        }
        _logger.debug('API Request: ${options.method} ${options.path}');
        return handler.next(options);
      },
      onResponse: (response, handler) {
        _logger.debug(
          'API Response: ${response.statusCode} ${response.requestOptions.path}',
        );
        return handler.next(response);
      },
      onError: (error, handler) {
        _logger.error(
          'API Error: ${error.message} ${error.requestOptions.path}',
        );
        return handler.next(error);
      },
    );
    _dio.interceptors.add(authInterceptor);
    _uploadDio.interceptors.add(authInterceptor);
  }

  static FmRepository isolateInit(String baseUrl, AuthToken token) {
    final inst = FmRepository._();
    inst._initWithToken(baseUrl, token);
    return inst;
  }

  void setBaseUrl(String baseUrl) {
    _baseUrl = baseUrl;
  }

  // ==================== Search ====================

  /// 搜索文件（按文件名模糊匹配）
  Future<ApiResult<SearchFilesRespData>> searchFiles({
    required String keyword,
    int offset = 0,
    int pageSize = 50,
    SearchType searchType = SearchType.NAME,
  }) async {
    try {
      final req = SearchFilesReq()
        ..keyword = keyword
        ..searchType = searchType;
      if (offset > 0) req.offset = offset;
      if (pageSize != 50) req.pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/file/search',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<SearchFilesRespData>(
        data,
        SearchFilesRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('搜索文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('搜索文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== File Service ====================

  /// 获取文件列表
  /// [parentId] 父目录 ID，为空时获取根目录
  /// [offset] 偏移量，从0开始
  /// [pageSize] 每页数量，默认500
  Future<ApiResult<FileListRespData>> getFileList({
    String parentId = '',
    int offset = 0,
    int? pageSize,
  }) async {
    try {
      final req = FileListReq(parentId: parentId);
      if (offset > 0) req.offset = offset;
      if (pageSize != null) req.pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/file/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<FileListRespData>(
        data,
        FileListRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取文件列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e, st) {
      _logger.error('获取文件列表失败: $e\n$st');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取文件夹列表（用于移动文件选择目标文件夹）
  Future<ApiResult<FolderListRespData>> getFolderList({
    required String parentId,
  }) async {
    try {
      final req = FolderListReq(parentId: parentId);
      final response = await _dio.post(
        '$_baseUrl/api/file/folders',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<FolderListRespData>(
        data,
        FolderListRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取文件夹列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取文件夹列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 创建目录
  /// [enableEncryption] 加密设置，null 表示从父文件夹继承
  /// [providerIds] 绑定的存储提供者（至少 1 个，须 ⊆ 管理员分配给自己的列表）；
  /// null/空表示不传 → 由后端继承父文件夹的绑定
  Future<ApiResult<CreateDirRespData>> createDir({
    required String name,
    required String parentId,
    bool? enableEncryption,
    int? maxHistories,
    List<int>? providerIds,
  }) async {
    try {
      final req = CreateDirReq(
        name: name,
        parentId: parentId,
        maxHistories: maxHistories,
      );
      // 只有明确设置了加密属性才传递
      if (enableEncryption != null) {
        req.enableEncryption = enableEncryption;
      }
      if (providerIds != null && providerIds.isNotEmpty) {
        req.providerIds.addAll(providerIds);
      }
      final response = await _dio.post(
        '$_baseUrl/api/file/create-dir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<CreateDirRespData>(
        data,
        CreateDirRespData(),
      );

      return ApiResult(data: respData, success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('创建目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('创建目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新目录
  /// [providerIds] 非空时更新存储提供者绑定（至少 1 个，须 ⊆ 管理员分配列表）；
  /// null/空表示保持原绑定不变
  Future<ApiResult<void>> updateDir({
    required String id,
    String? name,
    bool? enableEncryption,
    int? maxHistories,
    List<int>? providerIds,
  }) async {
    try {
      final req = UpdateDirReq()..id = id;
      if (name != null) req.name = name;
      if (enableEncryption != null) req.enableEncryption = enableEncryption;
      if (maxHistories != null) req.maxHistories = maxHistories;
      if (providerIds != null && providerIds.isNotEmpty) {
        req.providerIds.addAll(providerIds);
      }

      final response = await _dio.post(
        '$_baseUrl/api/file/update-dir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除文件
  Future<ApiResult<void>> deleteFile(List<String> ids) async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/file/delete',
        data: (DeleteFileReq()..ids.addAll(ids)).toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 重命名文件
  Future<ApiResult<void>> renameFile(List<RenameObj> renameItems) async {
    try {
      final req = RenameFileReq(renameObjs: renameItems);

      final response = await _dio.post(
        '$_baseUrl/api/file/rename',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('重命名文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('重命名文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取文件基本信息（轻量接口，只返回id/name/parent_id/type/size）
  Future<ApiResult<GetFileInfoRespData>> getFileInfo(String fileId) async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/file/info',
        data: {'file_id': fileId},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetFileInfoRespData>(
        data,
        GetFileInfoRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取文件基本信息失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取文件基本信息失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取文件详情
  Future<ApiResult<FileDetailRespData>> getFileDetail(String fileId) async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/file/detail',
        data: {'file_id': fileId},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<FileDetailRespData>(
        data,
        FileDetailRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取文件详情失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取文件详情失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // /// 上传文件
  // /// 返回文件 ID 和是否已存在（通过哈希判断）
  // Future<ApiResult<UploadRespData>> uploadFile({
  //   required String name,
  //   String? parentId,
  //   required int size,
  //   String? hash,
  //   required String mime,
  //   required List<int> fileBytes,
  //   void Function(int, int)? onSendProgress,
  // }) async {
  //   try {
  //     final metadata = UploadMetadata()
  //       ..name = name
  //       ..size = Int64(size)
  //       ..mime = mime;
  //     if (parentId != null) metadata.parentId = parentId;
  //     if (hash != null) metadata.hash = hash;

  //     final formData = FormData.fromMap({
  //       'metadata': metadata.toProto3Json(),
  //       'file': MultipartFile.fromBytes(fileBytes, filename: name),
  //     });

  //     final response = await _dio.post(
  //       '$_baseUrl/native.FileService/UploadFile',
  //       data: formData,
  //       onSendProgress: onSendProgress,
  //     );

  //     final data = response.data as Map<String, dynamic>;
  //     final respData = extractRespData<UploadRespData>(data, UploadRespData());
  //     return ApiResult(success: true, data: respData);
  //   } on DioException catch (e) {
  //     _logger.error('上传文件失败: ${e.message}');
  //     return ApiResult(success: false, message: _handleDioError(e));
  //   } catch (e) {
  //     _logger.error('上传文件失败: $e');
  //     return ApiResult(success: false, message: e.toString());
  //   }
  // }

  /// 处理 Dio 错误
  String _handleDioError(DioException e) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
        return '连接超时，请检查网络';
      case DioExceptionType.sendTimeout:
        return '发送超时，请重试';
      case DioExceptionType.receiveTimeout:
        return '接收超时，请重试';
      case DioExceptionType.badResponse:
        final statusCode = e.response?.statusCode;
        if (statusCode == 401) {
          return '登录已过期，请重新登录';
        }
        if (statusCode == 403) {
          return '没有权限执行此操作';
        }
        if (statusCode == 404) {
          return '接口不存在，请检查服务器配置';
        }
        if (statusCode != null && statusCode >= 500) {
          return '服务器错误，请稍后重试';
        }
        final data = e.response?.data;
        if (data is Map<String, dynamic>) {
          return data['message']?.toString() ?? '请求失败';
        }
        return '请求失败 (${statusCode ?? "未知状态码"})';
      case DioExceptionType.cancel:
        return '请求已取消';
      case DioExceptionType.connectionError:
        return '连接失败，请检查网络';
      default:
        return e.message ?? '未知错误';
    }
  }

  // ==================== Share Link ====================

  /// 创建分享链接
  Future<ApiResult<CreateShareLinkRespData>> createShareLink({
    required String fileId,
    int expireSeconds = 604800,
    bool neverExpire = false,
    int maxAccessCount = 0,
    String remark = '',
  }) async {
    try {
      final req = CreateShareLinkReq(
        fileId: fileId,
        expireSeconds: Int64(expireSeconds),
        neverExpire: neverExpire,
        maxAccessCount: maxAccessCount,
        remark: remark,
      );

      final response = await _dio.post(
        '$_baseUrl/api/file/share/create',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<CreateShareLinkRespData>(
        data,
        CreateShareLinkRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('创建分享链接失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('创建分享链接失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 取消分享链接
  Future<ApiResult<void>> cancelShareLink(String shareCode) async {
    try {
      final req = CancelShareLinkReq(shareCode: shareCode);

      final response = await _dio.post(
        '$_baseUrl/api/file/share/cancel',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('取消分享链接失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('取消分享链接失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取我的分享列表
  Future<ApiResult<ListMySharesRespData>> listMyShares({
    int offset = 0,
    int pageSize = 20,
  }) async {
    try {
      final req = ListMySharesReq(offset: offset, pageSize: pageSize);

      final response = await _dio.post(
        '$_baseUrl/api/file/share/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListMySharesRespData>(
        data,
        ListMySharesRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取我的分享列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取我的分享列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新分享链接（有效期/永久/访问上限/备注）
  /// 各参数传 null 表示保持原值；[maxAccessCount] 传 0 表示不限制访问次数
  Future<ApiResult<UpdateShareLinkRespData>> updateShareLink({
    required String shareCode,
    int? expireSeconds,
    bool? neverExpire,
    int? maxAccessCount,
    String? remark,
  }) async {
    try {
      final req = UpdateShareLinkReq(shareCode: shareCode);
      if (expireSeconds != null) {
        req.expireSeconds = Int64(expireSeconds);
      }
      if (neverExpire != null) req.neverExpire = neverExpire;
      if (maxAccessCount != null) req.maxAccessCount = maxAccessCount;
      if (remark != null) req.remark = remark;

      final response = await _dio.post(
        '$_baseUrl/api/file/share/update',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UpdateShareLinkRespData>(
        data,
        UpdateShareLinkRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新分享链接失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新分享链接失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== Trash (Recycle Bin) ====================

  /// 从回收站恢复文件
  Future<ApiResult<void>> restoreFromTrash(List<String> ids) async {
    try {
      final req = RestoreFromTrashReq()..ids.addAll(ids);

      final response = await _dio.post(
        '$_baseUrl/api/file/trash/restore',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('从回收站恢复失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('从回收站恢复失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 彻底删除文件（从回收站永久删除）
  Future<ApiResult<void>> deletePermanently(List<String> ids) async {
    try {
      final req = DeletePermanentlyReq()..ids.addAll(ids);

      final response = await _dio.post(
        '$_baseUrl/api/file/trash/delete-permanently',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('彻底删除失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('彻底删除失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 清空回收站
  Future<ApiResult<void>> emptyTrash() async {
    try {
      final req = EmptyTrashReq();

      final response = await _dio.post(
        '$_baseUrl/api/file/trash/empty',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('清空回收站失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('清空回收站失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除文件历史版本
  Future<ApiResult<void>> deleteFileHistory(List<int> ids) async {
    try {
      final req = DeleteFileHistoryReq()
        ..ids.addAll(ids.map((id) => Int64(id)));

      final response = await _dio.post(
        '$_baseUrl/api/file/history/delete',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除文件历史版本失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除文件历史版本失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 恢复文件历史版本
  /// [historyId] 历史版本ID；[newName] 为空表示覆盖当前文件，非空表示以新名称创建副本
  Future<ApiResult<void>> restoreFileHistory({
    required int historyId,
    String? newName,
  }) async {
    try {
      final req = RestoreFileHistoryReq()
        ..historyId = Int64(historyId);
      if (newName != null && newName.trim().isNotEmpty) {
        req.newName = newName.trim();
      }

      final response = await _dio.post(
        '$_baseUrl/api/file/history/restore',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('恢复文件历史版本失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('恢复文件历史版本失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取文件历史版本下载 URL
  String getFileHistoryDownloadUrl(int historyId) {
    return '$_baseUrl/api/file/history/download?id=$historyId';
  }

  // ==================== 离线下载（任务14） ====================

  /// 创建离线下载任务
  Future<ApiResult<CreateOfflineDownloadResp>> createOfflineDownload({
    required String url,
    String? parentId,
    String? fileName,
  }) async {
    try {
      final req = CreateOfflineDownloadReq()
        ..url = url.trim();
      if (parentId != null && parentId.isNotEmpty) {
        req.parentId = parentId;
      }
      if (fileName != null && fileName.trim().isNotEmpty) {
        req.fileName = fileName.trim();
      }

      final response = await _dio.post(
        '$_baseUrl/api/download/offline/create',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<CreateOfflineDownloadResp>(
        data,
        CreateOfflineDownloadResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('创建离线下载任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('创建离线下载任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 离线下载任务列表
  Future<ApiResult<ListOfflineDownloadsResp>> listOfflineDownloads({
    int page = 1,
    int pageSize = 20,
  }) async {
    try {
      final req = ListOfflineDownloadsReq()
        ..page = page
        ..pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/download/offline/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListOfflineDownloadsResp>(
        data,
        ListOfflineDownloadsResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取离线下载列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取离线下载列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除离线下载任务
  Future<ApiResult<void>> deleteOfflineDownload(int id) async {
    try {
      final req = DeleteOfflineDownloadReq()..id = id;
      final response = await _dio.post(
        '$_baseUrl/api/download/offline/delete',
        data: req.toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除离线下载任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除离线下载任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 重试失败的离线下载任务
  Future<ApiResult<void>> retryOfflineDownload(int id) async {
    try {
      final req = RetryOfflineDownloadReq()..id = id;
      final response = await _dio.post(
        '$_baseUrl/api/download/offline/retry',
        data: req.toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('重试离线下载任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('重试离线下载任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取回收站文件列表
  Future<ApiResult<TrashListRespData>> getTrashList() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/file/trash/list',
        data: {},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<TrashListRespData>(
        data,
        TrashListRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取回收站列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取回收站列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 移动文件
  /// [fileIds] 要移动的文件ID列表
  /// [targetParentId] 目标文件夹ID
  Future<ApiResult<MoveFileRespData>> moveFile({
    required List<String> fileIds,
    required String targetParentId,
  }) async {
    try {
      final req = MoveFileReq(fileIds: fileIds, targetParentId: targetParentId);
      final response = await _dio.post(
        '$_baseUrl/api/file/move',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<MoveFileRespData>(
        data,
        MoveFileRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('移动文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('移动文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== Storage Provider Service ====================

  /// 获取存储提供者列表
  Future<ApiResult<StorageProviderListRespData>> listStorageProviders() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/admin/provider/list',
        data: Empty().toProto3Json(),
      );
      _logger.info('获取存储提供者列表成功 $response');
      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<StorageProviderListRespData>(
        data,
        StorageProviderListRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取存储提供者列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取存储提供者列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取支持的存储提供者类型列表
  Future<ApiResult<GetStorageProviderTypesRespData>>
  getStorageProviderTypes() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/admin/provider/types',
        data: GetStorageProviderTypesReq().toProto3Json(),
      );
      _logger.info('获取存储提供者类型列表成功');
      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetStorageProviderTypesRespData>(
        data,
        GetStorageProviderTypesRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取存储提供者类型列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取存储提供者类型列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 添加存储提供者
  Future<ApiResult<void>> addStorageProvider({
    required StorageProviderType providerType,
    required String name,
    String? description,
    String? config,
    bool? enabled,
    int? sortOrder,
    int? totalCapacity,
    int? fileSizeLimit,
    String? encryptionKey,
    String? encryptionSuffix,
    bool? defaultBound,
    String? proxy,
    int? maxConcurrentUploads,
  }) async {
    try {
      final req = AddStorageProviderReq(providerType: providerType, name: name);

      if (description != null) req.description = description;
      if (config != null) req.config = config;
      if (enabled != null) req.enabled = enabled;
      if (sortOrder != null) req.sortOrder = sortOrder;
      if (totalCapacity != null) req.totalCapacity = Int64(totalCapacity);
      if (fileSizeLimit != null) req.fileSizeLimit = Int64(fileSizeLimit);
      if (encryptionKey != null) req.encryptionKey = encryptionKey;
      if (encryptionSuffix != null) req.encryptionSuffix = encryptionSuffix;
      if (defaultBound != null) req.defaultBound = defaultBound;
      if (proxy != null) req.proxy = proxy;
      if (maxConcurrentUploads != null) {
        req.maxConcurrentUploads = maxConcurrentUploads;
      }

      final response = await _dio.post(
        '$_baseUrl/api/admin/provider/add',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('添加存储提供者失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('添加存储提供者失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新存储提供者
  Future<ApiResult<void>> updateStorageProvider({
    required int id,
    String? name,
    String? description,
    String? config,
    bool? enabled,
    int? sortOrder,
    int? totalCapacity,
    int? fileSizeLimit,
    bool? defaultBound,
    String? proxy,
    int? maxConcurrentUploads,
  }) async {
    try {
      final req = UpdateStorageProviderReq(id: id);
      if (name != null) req.name = name;
      if (description != null) req.description = description;
      if (config != null) req.config = config;
      if (enabled != null) req.enabled = enabled;
      if (sortOrder != null) req.sortOrder = sortOrder;
      if (totalCapacity != null) req.totalCapacity = Int64(totalCapacity);
      if (fileSizeLimit != null) req.fileSizeLimit = Int64(fileSizeLimit);
      if (defaultBound != null) req.defaultBound = defaultBound;
      if (proxy != null) req.proxy = proxy;
      if (maxConcurrentUploads != null) {
        req.maxConcurrentUploads = maxConcurrentUploads;
      }

      final response = await _dio.post(
        '$_baseUrl/api/admin/provider/update',
        data: req.toProto3Json(),
      );
      _logger.info('更新存储提供者成功,$response');
      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新存储提供者失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新存储提供者失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除存储提供者
  Future<ApiResult<void>> deleteStorageProvider(List<int> ids) async {
    try {
      final req = StorageProviderDeleteReq()..ids.addAll(ids);

      final response = await _dio.post(
        '$_baseUrl/api/admin/provider/delete',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除存储提供者失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除存储提供者失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== Chunk Upload Service ====================

  /// 小文件上传（< 10MB）
  /// 单次请求完成上传，支持秒传检测
  Future<ApiResult<UploadSmallRespData>> uploadSmall({
    required String name,
    required String parentId,
    required int size,
    required String hash,
    required List<int> content,
    bool overwrite = false,
    String? tags,
    int? uploadId,
    void Function(int, int)? onSendProgress,
  }) async {
    try {
      final req = UploadSmallReq()
        ..name = name
        ..parentId = parentId
        ..size = Int64(size)
        ..hash = hash
        ..content = content
        ..overwrite = overwrite;
      if (tags != null && tags.isNotEmpty) {
        req.tags = tags;
      }
      if (uploadId != null && uploadId > 0) {
        req.uploadId = uploadId;
      }

      final response = await _uploadDio.post(
        '$_uploadBaseUrl/api/upload/small',
        data: req.toProto3Json(),
        onSendProgress: onSendProgress,
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UploadSmallRespData>(
        data,
        UploadSmallRespData(),
      );
      _logger.info('小文件上传成功: ${respData.fileId}, exists=${respData.exists}');
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('小文件上传失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('小文件上传失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 初始化上传任务
  /// 返回 upload_id 和已上传分片信息（支持断点续传）
  Future<ApiResult<InitUploadRespData>> initUpload({
    required String name,
    String? parentId,
    required int size,
    String? hash,
    int? chunkSize,
    bool overwrite = false,
    String? tags,
    int? uploadId,
  }) async {
    try {
      final req = InitUploadReq()
        ..name = name
        ..overwrite = overwrite
        ..size = Int64(size);
      if (parentId != null) req.parentId = parentId;
      if (hash != null) req.hash = hash;
      if (tags != null && tags.isNotEmpty) req.tags = tags;
      if (chunkSize != null && chunkSize > 0) {
        req.chunkSize = Int64(chunkSize);
      }
      if (uploadId != null && uploadId > 0) {
        req.uploadId = uploadId;
      }

      final response = await _uploadDio.post(
        '$_uploadBaseUrl/api/upload/init',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<InitUploadRespData>(
        data,
        InitUploadRespData(),
      );
      _logger.info('初始化上传任务成功: ${respData.uploadId}');
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('初始化上传任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('初始化上传任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 上传分片
  /// [uploadId] 上传任务ID
  /// [chunkIndex] 分片序号（从0开始）
  /// [chunkData] 分片数据
  /// [chunkHash] 分片哈希（可选，用于校验）
  Future<ApiResult<void>> uploadChunk({
    required int uploadId,
    required int chunkIndex,
    required List<int> chunkData,
    String? chunkHash,
    void Function(int, int)? onSendProgress,
  }) async {
    try {
      final req = UploadChunkReq()
        ..uploadId = uploadId
        ..chunkIndex = chunkIndex
        ..chunk = chunkData;
      if (chunkHash != null) req.chunkHash = chunkHash;

      final response = await _uploadDio.post(
        '$_uploadBaseUrl/api/upload/chunk',
        data: req.toProto3Json(),
        onSendProgress: onSendProgress,
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      _logger.info('上传分片成功: uploadId=$uploadId, chunkIndex=$chunkIndex');
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('上传分片失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('上传分片失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 完成上传
  Future<ApiResult<void>> completeUpload({required int uploadId}) async {
    try {
      final req = CompleteUploadReq()..uploadId = uploadId;

      final response = await _uploadDio.post(
        '$_uploadBaseUrl/api/upload/complete',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      _logger.info('完成上传成功: uploadId=$uploadId');
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('完成上传失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('完成上传失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 取消上传
  Future<ApiResult<void>> cancelUpload({required int uploadId}) async {
    try {
      final req = CancelUploadReq()..uploadId = uploadId;

      final response = await _dio.post(
        '$_baseUrl/api/upload/cancel',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      _logger.info('取消上传成功: $uploadId');
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('取消上传失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('取消上传失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取上传记录列表
  Future<ApiResult<ListUploadRecordsResp>> listUploadRecords({
    int page = 1,
    int pageSize = 20,
    int? status,
  }) async {
    try {
      final req = ListUploadRecordsReq(page: page, pageSize: pageSize);
      if (status != null) {
        req.status = UploadStatus.valueOf(status)!;
      }

      final response = await _dio.post(
        '$_baseUrl/api/upload/records/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListUploadRecordsResp>(
        data,
        ListUploadRecordsResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取上传记录列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取上传记录列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取上传记录详情
  Future<ApiResult<GetUploadRecordDetailResp>> getUploadRecordDetail(
    int id,
  ) async {
    try {
      final req = GetUploadRecordDetailReq(id: id);

      final response = await _dio.post(
        '$_baseUrl/api/upload/records/detail',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetUploadRecordDetailResp>(
        data,
        GetUploadRecordDetailResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取上传记录详情失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取上传记录详情失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 查询文件缓存状态
  Future<ApiResult<GetFileCacheStatusResp>> getFileCacheStatus({
    required String fileHash,
    required int providerId,
  }) async {
    try {
      final req = GetFileCacheStatusReq(
        fileHash: fileHash,
        providerId: providerId,
      );

      final response = await _dio.post(
        '$_baseUrl/api/file/cache-status',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetFileCacheStatusResp>(
        data,
        GetFileCacheStatusResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('查询文件缓存状态失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('查询文件缓存状态失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 重试上传记录
  Future<ApiResult<void>> retryUploadRecord(int id) async {
    try {
      final req = RetryUploadRecordReq(id: id);

      final response = await _dio.post(
        '$_baseUrl/api/upload/records/retry',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('重试上传记录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('重试上传记录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 重试所有失败的上传记录
  Future<ApiResult<void>> retryAllFailedUploadRecords() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/upload/records/retry-all',
        data: Empty().toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('重试所有失败记录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('重试所有失败记录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除上传记录
  Future<ApiResult<void>> deleteUploadRecord(int id) async {
    try {
      final req = DeleteUploadRecordReq(id: id);

      final response = await _dio.post(
        '$_baseUrl/api/upload/records/delete',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除上传记录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除上传记录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取可用于同步的存储提供者列表
  /// 返回：已启用且文件尚未上传到的提供者
  Future<ApiResult<List<StorageProvider>>> getAvailableProvidersForSync(
    String fileHash,
  ) async {
    try {
      final req = GetAvailableProvidersReq()..fileHash = fileHash;

      final response = await _dio.post(
        '$_baseUrl/api/file/sync-providers',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final resp = extractRespData<GetAvailableProvidersResp>(
        data,
        GetAvailableProvidersResp(),
      );

      return ApiResult(success: true, data: resp.providers);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取可用提供者失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取可用提供者失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 同步文件到指定存储提供者
  Future<ApiResult<void>> syncFileToProviders(
    String fileId,
    List<int> providerIds,
  ) async {
    try {
      final req = SyncFileToProvidersReq()
        ..fileId = fileId
        ..providerIds.addAll(providerIds.map((id) => id));

      final response = await _dio.post(
        '$_baseUrl/api/file/sync',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());

      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('同步文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('同步文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新文件标签
  Future<ApiResult<void>> updateFileTags({
    required String fileId,
    required List<String> tags,
  }) async {
    try {
      final req = UpdateFileTagsReq()
        ..fileId = fileId
        ..tags.addAll(tags);

      final response = await _dio.post(
        '$_baseUrl/api/file/tags/update',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新文件标签失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新文件标签失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取文件标签
  Future<ApiResult<List<String>>> getFileTags({required String fileId}) async {
    try {
      final req = GetFileTagsReq()..fileId = fileId;

      final response = await _dio.post(
        '$_baseUrl/api/file/tags/get',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetFileTagsRespData>(
        data,
        GetFileTagsRespData(),
      );
      return ApiResult(success: true, data: respData.tags);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取文件标签失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取文件标签失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== 备份管理 API ====================

  /// 创建备份任务
  Future<ApiResult<void>> createBackupTask({
    required String name,
    required String sourcePath,
    required String targetDirId,
    BackupStrategy? strategy,
    String? cronExpression,
    String? ignoreRules,
    bool? recursive,
    bool? enabled,
  }) async {
    try {
      final req = CreateBackupTaskReq()
        ..name = name
        ..sourcePath = sourcePath
        ..targetDirId = targetDirId;
      if (strategy != null) req.strategy = strategy;
      if (cronExpression != null) req.cronExpression = cronExpression;
      if (ignoreRules != null) req.ignoreRules = ignoreRules;
      if (recursive != null) req.recursive = recursive;
      if (enabled != null) req.enabled = enabled;

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/task/create',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('创建备份任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('创建备份任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新备份任务
  Future<ApiResult<void>> updateBackupTask({
    required int id,
    String? name,
    String? sourcePath,
    String? targetDirId,
    BackupStrategy? strategy,
    String? cronExpression,
    String? ignoreRules,
    bool? recursive,
    bool? enabled,
  }) async {
    try {
      final req = UpdateBackupTaskReq()..id = Int64(id);
      if (name != null) req.name = name;
      if (sourcePath != null) req.sourcePath = sourcePath;
      if (targetDirId != null) req.targetDirId = targetDirId;
      if (strategy != null) req.strategy = strategy;
      if (cronExpression != null) req.cronExpression = cronExpression;
      if (ignoreRules != null) req.ignoreRules = ignoreRules;
      if (recursive != null) req.recursive = recursive;
      if (enabled != null) req.enabled = enabled;

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/task/update',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新备份任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新备份任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除备份任务
  Future<ApiResult<void>> deleteBackupTask(List<int> ids) async {
    try {
      final req = DeleteBackupTaskReq()..ids.addAll(ids.map((id) => Int64(id)));

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/task/delete',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('删除备份任务失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除备份任务失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取备份任务列表
  Future<ApiResult<ListBackupTasksRespData>> listBackupTasks({
    int? page,
    int? pageSize,
    bool? enabled,
  }) async {
    try {
      final req = ListBackupTasksReq();
      if (page != null) req.page = page;
      if (pageSize != null) req.pageSize = pageSize;
      if (enabled != null) req.enabled = enabled;

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/task/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListBackupTasksRespData>(
        data,
        ListBackupTasksRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取备份任务列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取备份任务列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取备份任务详情
  Future<ApiResult<GetBackupTaskRespData>> getBackupTask(int id) async {
    try {
      final req = GetBackupTaskReq()..id = Int64(id);

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/task/get',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetBackupTaskRespData>(
        data,
        GetBackupTaskRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取备份任务详情失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取备份任务详情失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 手动触发备份
  Future<ApiResult<void>> triggerBackup(int id) async {
    try {
      final req = TriggerBackupReq()..id = Int64(id);

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/trigger',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('触发备份失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('触发备份失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取备份文件记录
  Future<ApiResult<ListBackupFilesRespData>> listBackupFiles({
    required int taskId,
    int? page,
    int? pageSize,
    BackupFileStatus? status,
  }) async {
    try {
      final req = ListBackupFilesReq()..taskId = Int64(taskId);
      if (page != null) req.page = page;
      if (pageSize != null) req.pageSize = pageSize;
      if (status != null) req.status = status;

      final response = await _dio.post(
        '$_baseUrl/api/admin/backup/files',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListBackupFilesRespData>(
        data,
        ListBackupFilesRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取备份文件记录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取备份文件记录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取最近使用的文件
  Future<ApiResult<GetRecentFilesRespData>> getRecentFiles({
    int offset = 0,
    int? pageSize,
  }) async {
    try {
      final req = GetRecentFilesReq();
      if (offset > 0) req.offset = offset;
      if (pageSize != null) req.pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/file/recent',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetRecentFilesRespData>(
        data,
        GetRecentFilesRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取最近使用文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取最近使用文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 按文件类型获取文件列表
  Future<ApiResult<GetFilesByTypeRespData>> getFilesByType({
    required FileType fileType,
    int offset = 0,
    int? pageSize,
  }) async {
    try {
      final req = GetFilesByTypeReq()..fileType = fileType;
      if (offset > 0) req.offset = offset;
      if (pageSize != null) req.pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/file/by-type',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetFilesByTypeRespData>(
        data,
        GetFilesByTypeRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('按类型获取文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('按类型获取文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取有空位的编号子目录（相册备份用）
  /// 返回 parentId 下第一个文件数 < 500 的编号目录 ID；
  /// 若都满了，按最大编号 +1 自动创建新目录（命名 001、002…）
  Future<ApiResult<String>> getOrCreateSubfolder({
    required String parentId,
  }) async {
    try {
      final req = GetOrCreateSubfolderReq()..parentId = parentId;

      final response = await _dio.post(
        '$_baseUrl/api/file/get-or-create-subfolder',
        data: req.toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetOrCreateSubfolderResp>(
        data,
        GetOrCreateSubfolderResp(),
      );
      final folderId = respData.folderId;
      if (folderId.isEmpty) {
        return ApiResult(success: false, message: '未返回目录ID');
      }
      return ApiResult(success: true, data: folderId);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取子目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取子目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 按相对路径逐级确保目录存在（mkdir -p 语义，后端自动创建，并发安全）
  /// 相册备份按"年/月"归档用，如 path = "2026/07"
  /// 返回叶子目录 ID
  Future<ApiResult<String>> ensureDirPath({
    required String parentId,
    required String path,
  }) async {
    try {
      final req = EnsureDirPathReq()
        ..parentId = parentId
        ..path = path;

      final response = await _dio.post(
        '$_baseUrl/api/file/ensure-dir-path',
        data: req.toProto3Json(),
      );
      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<EnsureDirPathResp>(
        data,
        EnsureDirPathResp(),
      );
      final folderId = respData.folderId;
      if (folderId.isEmpty) {
        return ApiResult(success: false, message: '未返回目录ID');
      }
      return ApiResult(success: true, data: folderId);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('确保目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('确保目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }




  // ==================== UserSpace Service ====================

  /// 获取当前用户的空间信息
  Future<ApiResult<GetUserSpaceResp>> getUserSpace() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/space/get',
        data: {},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetUserSpaceResp>(
        data,
        GetUserSpaceResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取用户空间信息失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取用户空间信息失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 保存分享到我的空间
  Future<ApiResult<AsyncTaskProgressResp>> saveShareToMySpace({
    required String shareCode,
    required String targetParentId,
    String? displayName,
  }) async {
    try {
      final req = SaveShareToMySpaceReq()
        ..shareCode = shareCode
        ..targetParentId = targetParentId;
      if (displayName != null) req.displayName = displayName;

      final response = await _dio.post(
        '$_baseUrl/api/space/save-share',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<AsyncTaskProgressResp>(
        data,
        AsyncTaskProgressResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('保存分享到空间失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('保存分享到空间失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取异步任务进度
  Future<ApiResult<AsyncTaskProgressResp>> getAsyncTaskProgress(
    String taskId,
  ) async {
    try {
      final req = AsyncTaskProgressReq()..taskId = taskId;

      final response = await _dio.post(
        '$_baseUrl/api/space/async-task-progress',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<AsyncTaskProgressResp>(
        data,
        AsyncTaskProgressResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取异步任务进度失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取异步任务进度失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 列出用户空间列表
  Future<ApiResult<ListUserSpacesResp>> listUserSpaces({
    int page = 0,
    int pageSize = 0,
  }) async {
    try {
      final req = ListUserSpacesReq();
      if (page > 0) req.page = page;
      if (pageSize > 0) req.pageSize = pageSize;

      final response = await _dio.post(
        '$_baseUrl/api/admin/user/spaces',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListUserSpacesResp>(
        data,
        ListUserSpacesResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('列出用户空间失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('列出用户空间失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 管理员：为用户分配根目录并绑定存储提供者
  Future<ApiResult<AssignRootDirResp>> assignRootDir({
    required String userId,
    required List<int> providerIds,
  }) async {
    try {
      final req = AssignRootDirReq()
        ..userId = userId
        ..providerIds.addAll(providerIds);

      final response = await _dio.post(
        '$_baseUrl/api/admin/user/assign-root',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<AssignRootDirResp>(
        data,
        AssignRootDirResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('分配根目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('分配根目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新用户空间配额
  Future<ApiResult<void>> updateUserSpace({
    required String userId,
    required int quotaBytes,
  }) async {
    try {
      final req = UpdateUserSpaceReq()
        ..userId = userId
        ..quotaBytes = Int64(quotaBytes);

      final response = await _dio.post(
        '$_baseUrl/api/admin/user/update-space',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新用户空间失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新用户空间失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取用户列表（UserSpace 版本）
  Future<ApiResult<ListUsersResp>> listUsers() async {
    try {
      final req = ListUsersReq();

      final response = await _dio.post(
        '$_baseUrl/api/admin/user/list',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<ListUsersResp>(data, ListUsersResp());
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取用户列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取用户列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取当前用户角色（走 UserSpaceService）
  Future<ApiResult<GetMyRolesResp>> getMyRoles() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/space/my-roles',
        data: {},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetMyRolesResp>(data, GetMyRolesResp());
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取用户角色失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取用户角色失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取管理员分配给当前用户的存储提供者（文件夹绑定选择用，仅启用的）
  Future<ApiResult<GetMyBoundProvidersResp>> getMyBoundProviders() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/api/space/my-providers',
        data: {},
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<GetMyBoundProvidersResp>(
        data,
        GetMyBoundProvidersResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('获取可用存储提供者失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取可用存储提供者失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新当前用户资料（用户名/邮箱，仅本人可改）
  Future<ApiResult<UpdateProfileResp>> updateProfile({
    required String username,
    String email = '',
  }) async {
    try {
      final req = UpdateProfileReq()
        ..username = username
        ..email = email;

      final response = await _dio.post(
        '$_baseUrl/api/space/profile/update',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UpdateProfileResp>(
        data,
        UpdateProfileResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('更新资料失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新资料失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 上传当前用户头像（multipart/form-data，字段名 file）
  Future<ApiResult<UploadAvatarResp>> uploadAvatar({
    required String filePath,
  }) async {
    try {
      final form = FormData.fromMap({
        'file': await MultipartFile.fromFile(filePath),
      });

      final response = await _uploadDio.post(
        '$_baseUrl/api/user/profile/avatar',
        data: form,
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UploadAvatarResp>(
        data,
        UploadAvatarResp(),
      );
      return ApiResult(success: true, data: respData);
    } on TokenInvalidException {
      await _handleTokenInvalid();
      return ApiResult(success: false, message: '登录已过期，请重新登录');
    } on DioException catch (e) {
      _logger.error('上传头像失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('上传头像失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 把服务端返回的相对头像路径拼成完整 URL（供 Image.network 使用）
  String? resolveAvatarUrl(String? avatar) {
    if (avatar == null || avatar.isEmpty) return null;
    if (avatar.startsWith('http://') || avatar.startsWith('https://')) {
      return avatar;
    }
    final base = _baseUrl ?? '';
    return '$base$avatar';
  }
}
