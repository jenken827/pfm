import 'dart:async';

import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:fixnum/fixnum.dart';
import 'package:pfm/models/models.dart';
import 'package:protobuf/well_known_types/google/protobuf/empty.pb.dart';
import 'package:talker_flutter/talker_flutter.dart';

import '../proto/native/enum.pbenum.dart';
import '../proto/native/file.pb.dart';
import '../proto/native/provider.pb.dart';
import '../proto/native/upload.pb.dart';
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
  Talker get _logger => getLogger();

  String? _baseUrl;

  FmRepository._();

  factory FmRepository() {
    final inst = FmRepository._();
    final ls = GetIt.I.get<LocalStorage>();
    inst._init(ls.fileServer, ls.token);
    return inst;
  }

  void _init(String? baseUrl, CasdoorToken? token) {
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

    // 添加拦截器
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          // 添加认证 Token
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
      ),
    );
  }

  static FmRepository isolateInit(String baseUrl, CasdoorToken token) {
    final inst = FmRepository._();
    inst._init(baseUrl, token);
    return inst;
  }

  void setBaseUrl(String baseUrl) {
    _baseUrl = baseUrl;
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
    } on DioException catch (e) {
      _logger.error('获取文件列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取文件列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 创建目录
  Future<ApiResult<void>> createDir({
    required String name,
    required String parentId,
    required bool enableEncryption,
    bool encryptFilename = false,
    List<int>? providerIds,
  }) async {
    try {
      final req = CreateDirReq(
        name: name,
        parentId: parentId,
        encryptFilename: encryptFilename,
        enableEncryption: enableEncryption,
        providerIds: providerIds,
      );
      final response = await _dio.post(
        '$_baseUrl/native.FileService/CreateDir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
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
    bool? encryptFilename,
    List<int>? providerIds,
  }) async {
    try {
      final req = UpdateDirReq()..id = id;
      if (name != null) req.name = name;
      if (encryptFilename != null) req.encryptFilename = encryptFilename;
      if (providerIds != null) req.providerIds.addAll(providerIds);

      final response = await _dio.post(
        '$_baseUrl/native.FileService/UpdateDir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
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
    } on DioException catch (e) {
      _logger.error('重命名文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('重命名文件失败: $e');
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
        if (statusCode != null && statusCode >= 500) {
          return '服务器错误，请稍后重试';
        }
        return e.response?.data?['message'] ?? '请求失败';
      case DioExceptionType.cancel:
        return '请求已取消';
      case DioExceptionType.connectionError:
        return '连接失败，请检查网络';
      default:
        return e.message ?? '未知错误';
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
    int? order,
    int? totalCapacity,
    int? fileSizeLimit,
    String? encryptionKey,
    bool? encryptFilename,
    String? encryptionSuffix,
    bool? defaultBound,
    String? proxy,
  }) async {
    try {
      final req = AddStorageProviderReq(
        providerType: providerType,
        name: name,
      );

      if (description != null) req.description = description;
      if (config != null) req.config = config;
      if (enabled != null) req.enabled = enabled;
      if (order != null) req.order = order;
      if (totalCapacity != null) req.totalCapacity = Int64(totalCapacity);
      if (fileSizeLimit != null) req.fileSizeLimit = Int64(fileSizeLimit);
      if (encryptionKey != null) req.encryptionKey = encryptionKey;
      if (encryptFilename != null) req.encryptFilename = encryptFilename;
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
    int? order,
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
      if (order != null) req.order = order;
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

      final response = await _dio.post(
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
  }) async {
    try {
      final req = InitUploadReq()
        ..name = name
        ..overwrite = overwrite
        ..size = Int64(size);
      if (parentId != null) req.parentId = parentId;
      if (hash != null) req.hash = hash;

      final response = await _dio.post(
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

      final response = await _dio.post(
        '$_baseUrl/native.UploadService/UploadChunk',
        data: req.toProto3Json(),
        onSendProgress: onSendProgress,
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      _logger.info('上传分片成功: uploadId=$uploadId, chunkIndex=$chunkIndex');
      return ApiResult(success: true);
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

      final response = await _dio.post(
        '$_baseUrl/native.UploadService/CompleteUpload',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      _logger.info('完成上传成功: uploadId=$uploadId');
      return ApiResult(success: true);
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
    } on DioException catch (e) {
      _logger.error('取消上传失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('取消上传失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }
}
