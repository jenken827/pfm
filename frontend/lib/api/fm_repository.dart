import 'dart:async';

import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:fixnum/fixnum.dart';
import 'package:pfm/models/models.dart';
import 'package:protobuf/well_known_types/google/protobuf/empty.pb.dart';
import 'package:talker_flutter/talker_flutter.dart';

import '../proto/native/backup.pb.dart';
import '../proto/native/enum.pbenum.dart';
import '../proto/native/file.pb.dart';
import '../proto/native/provider.pb.dart';
import '../proto/native/upload.pb.dart';
import '../router/router.dart';
import '../services/local_storage.dart';
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

  FmRepository._();

  factory FmRepository() {
    final inst = FmRepository._();
    final ls = GetIt.I.get<LocalStorage>();
    inst._init(ls.fileServer);
    return inst;
  }

  /// 处理 Token 失效
  Future<void> _handleTokenInvalid() async {
    _logger.error('Token invalid, clearing login info and redirecting to login');
    final ls = GetIt.I.get<LocalStorage>();
    await ls.clearLoginInfo();
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
        _logger.info('API Request: ${options.method} ${options.path}');
        return handler.next(options);
      },
      onResponse: (response, handler) {
        _logger.info(
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

  /// 用于 isolate 中初始化，需要手动传入 token
  void _initWithToken(String? baseUrl, CasdoorToken? token) {
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
        _logger.info('API Request: ${options.method} ${options.path}');
        return handler.next(options);
      },
      onResponse: (response, handler) {
        _logger.info(
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

  static FmRepository isolateInit(String baseUrl, CasdoorToken token) {
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
        '$_baseUrl/native.FileService/SearchFiles',
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
        '$_baseUrl/native.FileService/GetFileList',
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
    } catch (e) {
      _logger.error('获取文件列表失败: $e');
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
        '$_baseUrl/native.FileService/GetFolderList',
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
  Future<ApiResult<CreateDirRespData>> createDir({
    required String name,
    required String parentId,
    bool? enableEncryption,
    List<int>? providerIds,
    int? maxHistories,
  }) async {
    try {
      final req = CreateDirReq(
        name: name,
        parentId: parentId,
        providerIds: providerIds,
        maxHistories: maxHistories,
      );
      // 只有明确设置了加密属性才传递
      if (enableEncryption != null) {
        req.enableEncryption = enableEncryption;
      }
      final response = await _dio.post(
        '$_baseUrl/native.FileService/CreateDir',
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
  Future<ApiResult<void>> updateDir({
    required String id,
    String? name,
    bool? enableEncryption,
    List<int>? providerIds,
    int? maxHistories,
  }) async {
    try {
      final req = UpdateDirReq()..id = id;
      if (name != null) req.name = name;
      if (enableEncryption != null) req.enableEncryption = enableEncryption;
      if (providerIds != null) req.providerIds.addAll(providerIds);
      if (maxHistories != null) req.maxHistories = maxHistories;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/UpdateDir',
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
        '$_baseUrl/native.FileService/DeleteFile',
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
        '$_baseUrl/native.FileService/RenameFile',
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
        '$_baseUrl/native.FileService/GetFileInfo',
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
        '$_baseUrl/native.FileService/GetFileDetail',
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
        '$_baseUrl/native.FileService/CreateShareLink',
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
        '$_baseUrl/native.FileService/CancelShareLink',
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

  // ==================== Trash (Recycle Bin) ====================

  /// 从回收站恢复文件
  Future<ApiResult<void>> restoreFromTrash(List<String> ids) async {
    try {
      final req = RestoreFromTrashReq()..ids.addAll(ids);

      final response = await _dio.post(
        '$_baseUrl/native.FileService/RestoreFromTrash',
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
        '$_baseUrl/native.FileService/DeletePermanently',
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
        '$_baseUrl/native.FileService/EmptyTrash',
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
        '$_baseUrl/native.FileService/DeleteFileHistory',
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

  /// 获取文件历史版本下载 URL
  String getFileHistoryDownloadUrl(int historyId) {
    return '$_baseUrl/api/file/history/download?id=$historyId';
  }

  /// 获取回收站文件列表
  Future<ApiResult<TrashListRespData>> getTrashList() async {
    try {
      final response = await _dio.post(
        '$_baseUrl/native.FileService/GetTrashList',
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
        '$_baseUrl/native.FileService/MoveFile',
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
        '$_baseUrl/native.StorageProviderService/ListStorageProviders',
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
        '$_baseUrl/native.StorageProviderService/GetStorageProviderTypes',
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

      final response = await _dio.post(
        '$_baseUrl/native.StorageProviderService/AddStorageProvider',
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

      final response = await _dio.post(
        '$_baseUrl/native.StorageProviderService/UpdateStorageProvider',
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
        '$_baseUrl/native.StorageProviderService/DeleteStorageProvider',
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

      final response = await _uploadDio.post(
        '$_baseUrl/native.UploadService/UploadSmall',
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
  }) async {
    try {
      final req = InitUploadReq()
        ..name = name
        ..overwrite = overwrite
        ..size = Int64(size);
      if (parentId != null) req.parentId = parentId;
      if (hash != null) req.hash = hash;
      if (tags != null && tags.isNotEmpty) req.tags = tags;

      final response = await _uploadDio.post(
        '$_baseUrl/native.UploadService/InitUpload',
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
        '$_baseUrl/native.UploadService/UploadChunk',
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
        '$_baseUrl/native.UploadService/CompleteUpload',
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
        '$_baseUrl/native.UploadService/CancelUpload',
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
        req.status = LocationStatus.valueOf(status)!;
      }

      final response = await _dio.post(
        '$_baseUrl/native.UploadService/ListUploadRecords',
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
        '$_baseUrl/native.UploadService/GetUploadRecordDetail',
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
  Future<ApiResult<GetFileCacheStatusResp>> getFileCacheStatus(
    String fileHash,
    int providerId,
  ) async {
    try {
      final req = GetFileCacheStatusReq(
        fileHash: fileHash,
        providerId: providerId,
      );

      final response = await _dio.post(
        '$_baseUrl/native.FileService/GetFileCacheStatus',
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
        '$_baseUrl/native.UploadService/RetryUploadRecord',
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

  /// 删除上传记录
  Future<ApiResult<void>> deleteUploadRecord(int id) async {
    try {
      final req = DeleteUploadRecordReq(id: id);

      final response = await _dio.post(
        '$_baseUrl/native.UploadService/DeleteUploadRecord',
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
        '$_baseUrl/native.FileService/GetAvailableProvidersForSync',
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
        '$_baseUrl/native.FileService/SyncFileToProviders',
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
        '$_baseUrl/native.FileService/UpdateFileTags',
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
        '$_baseUrl/native.FileService/GetFileTags',
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
        '$_baseUrl/native.BackupService/CreateBackupTask',
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
        '$_baseUrl/native.BackupService/UpdateBackupTask',
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
        '$_baseUrl/native.BackupService/DeleteBackupTask',
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
        '$_baseUrl/native.BackupService/ListBackupTasks',
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
        '$_baseUrl/native.BackupService/GetBackupTask',
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
        '$_baseUrl/native.BackupService/TriggerBackup',
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
        '$_baseUrl/native.BackupService/ListBackupFiles',
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
        '$_baseUrl/native.FileService/GetRecentFiles',
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
        '$_baseUrl/native.FileService/GetFilesByType',
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
}