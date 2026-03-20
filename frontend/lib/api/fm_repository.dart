import 'dart:async';

import 'package:dio/dio.dart';
import 'package:get_it/get_it.dart';
import 'package:fixnum/fixnum.dart';
import 'package:protobuf/well_known_types/google/protobuf/empty.pb.dart';
import 'package:talker_flutter/talker_flutter.dart';

import '../proto/native/enum.pbenum.dart';
import '../proto/native/file.pb.dart';
import '../proto/native/group.pb.dart';
import '../proto/native/provider.pb.dart';
import '../proto/native/tables.pb.dart';
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

  String? get _baseUrl => GetIt.I.get<LocalStorage>().fileServer;

  FmRepository() {
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
          final token = GetIt.I.get<LocalStorage>().token;
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

  // ==================== File Service ====================

  /// 获取文件列表
  /// [parentId] 父目录 ID，为空时获取根目录
  Future<ApiResult<FileListRespData>> getFileList({
    String parentId = '',
  }) async {
    try {
      final response = await _dio.post(
        '$_baseUrl/native.FileService/GetFileList',
        data: FileListReq(parentId: parentId).toProto3Json(),
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
  Future<ApiResult<File>> createDir({
    required String name,
    required String parentId,
    required bool enableEncryption,
    bool encryptName = false,
    String? encryptionSuffix,
    String? encryptionKey,
    String? encryptionSalt,
  }) async {
    try {
      final req = CreateDirReq(
        name: name,
        parentId: parentId,
        encryptName: encryptName,
        enableEncryption: enableEncryption,
        encryptionSuffix: encryptionSuffix,
        encryptionKey: encryptionKey,
        encryptionSalt: encryptionSalt,
      );
      final response = await _dio.post(
        '$_baseUrl/native.FileService/CreateDir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<CreateDirRespData>(
        data,
        CreateDirRespData(),
      );
      return ApiResult(success: true, data: respData.detail);
    } on DioException catch (e) {
      _logger.error('创建目录失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('创建目录失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新目录
  Future<ApiResult<File>> updateDir({
    required String id,
    String? name,
    bool? encryptName,
    String? encryptionSuffix,
    String? encryptionKey,
    String? encryptionSalt,
  }) async {
    try {
      final req = UpdateDirReq()..id = id;
      if (name != null) req.name = name;
      if (encryptName != null) req.encryptName = encryptName;
      if (encryptionSuffix != null) req.encryptionSuffix = encryptionSuffix;
      if (encryptionKey != null) req.encryptionKey = encryptionKey;
      if (encryptionSalt != null) req.encryptionSalt = encryptionSalt;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/UpdateDir',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UpdateDirRespData>(
        data,
        UpdateDirRespData(),
      );
      return ApiResult(success: true, data: respData.detail);
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

  /// 上传文件
  /// 返回文件 ID 和是否已存在（通过哈希判断）
  Future<ApiResult<UploadRespData>> uploadFile({
    required String name,
    String? parentId,
    required int size,
    String? hash,
    required String mime,
    required List<int> fileBytes,
    void Function(int, int)? onSendProgress,
  }) async {
    try {
      final metadata = UploadMetadata()
        ..name = name
        ..size = Int64(size)
        ..mime = mime;
      if (parentId != null) metadata.parentId = parentId;
      if (hash != null) metadata.hash = hash;

      final formData = FormData.fromMap({
        'metadata': metadata.toProto3Json(),
        'file': MultipartFile.fromBytes(fileBytes, filename: name),
      });

      final response = await _dio.post(
        '$_baseUrl/native.FileService/UploadFile',
        data: formData,
        onSendProgress: onSendProgress,
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UploadRespData>(data, UploadRespData());
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('上传文件失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('上传文件失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

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

  // ==================== Storage Group Service ====================

  /// 获取存储组列表
  Future<ApiResult<StorageGroupListRespData>> listStorageGroups({
    int page = 1,
    int pageSize = 20,
    String? keyword,
    bool withStats = false,
  }) async {
    try {
      final req = StorageGroupListReq()
        ..page = page
        ..pageSize = pageSize
        ..withStats = withStats;
      if (keyword != null) req.keyword = keyword;

      final response = await _dio.post(
        '$_baseUrl/native.StorageGroupService/ListStorageGroups',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<StorageGroupListRespData>(
        data,
        StorageGroupListRespData(),
      );
      _logger.info('获取存储组列表成功 total ${respData.total}');
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('获取存储组列表失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取存储组列表失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 添加存储组
  Future<ApiResult<StorageGroupAddRespData>> addStorageGroup({
    required String name,
    String? description,
  }) async {
    try {
      final req = StorageGroupAddReq()..name = name;
      if (description != null) req.description = description;

      final response = await _dio.post(
        '$_baseUrl/native.StorageGroupService/AddStorageGroup',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<StorageGroupAddRespData>(
        data,
        StorageGroupAddRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('添加存储组失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('添加存储组失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 更新存储组
  Future<ApiResult<void>> updateStorageGroup({
    required int id,
    String? name,
    String? description,
  }) async {
    try {
      final req = StorageGroupUpdateReq()..id = id;
      if (name != null) req.name = name;
      if (description != null) req.description = description;

      final response = await _dio.post(
        '$_baseUrl/native.StorageGroupService/UpdateStorageGroup',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on DioException catch (e) {
      _logger.error('更新存储组失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('更新存储组失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 删除存储组
  Future<ApiResult<void>> deleteStorageGroup(
    int id, {
    bool force = false,
  }) async {
    try {
      final req = StorageGroupDeleteReq()
        ..id = id
        ..force = force;

      final response = await _dio.post(
        '$_baseUrl/native.StorageGroupService/DeleteStorageGroup',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      extractRespData<Empty>(data, Empty());
      return ApiResult(success: true);
    } on DioException catch (e) {
      _logger.error('删除存储组失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('删除存储组失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  // ==================== Storage Provider Service ====================

  /// 获取存储提供者列表
  Future<ApiResult<StorageProviderListRespData>> listStorageProviders({
    int page = 1,
    int pageSize = 20,
    String? keyword,
    StorageProviderType? providerType,
    int? groupId,
    bool enabledOnly = false,
  }) async {
    try {
      final req = StorageProviderListReq()
        ..page = page
        ..pageSize = pageSize
        ..enabledOnly = enabledOnly;
      if (keyword != null) req.keyword = keyword;
      if (providerType != null) req.providerType = providerType;
      if (groupId != null) req.groupId = groupId;

      final response = await _dio.post(
        '$_baseUrl/native.StorageProviderService/ListStorageProviders',
        data: req.toProto3Json(),
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

  /// 添加存储提供者
  Future<ApiResult<StorageProviderAddRespData>> addStorageProvider({
    required StorageProviderType providerType,
    required String name,
    String? description,
    String? config,
    required int groupId,
    bool enabled = true,
    int order = 0,
  }) async {
    try {
      final req = StorageProviderAddReq()
        ..providerType = providerType
        ..name = name
        ..groupId = groupId
        ..enabled = enabled
        ..order = order;
      if (description != null) req.description = description;
      if (config != null) req.config = config;

      final response = await _dio.post(
        '$_baseUrl/native.StorageProviderService/AddStorageProvider',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<StorageProviderAddRespData>(
        data,
        StorageProviderAddRespData(),
      );
      return ApiResult(success: true, data: respData);
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
    int? groupId,
    bool? enabled,
    int? order,
  }) async {
    try {
      final req = StorageProviderUpdateReq()..id = id;
      if (name != null) req.name = name;
      if (description != null) req.description = description;
      if (config != null) req.config = config;
      if (groupId != null) req.groupId = groupId;
      if (enabled != null) req.enabled = enabled;
      if (order != null) req.order = order;

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
  Future<ApiResult<void>> deleteStorageProvider(
    int id, {
    bool force = false,
  }) async {
    try {
      final req = StorageProviderDeleteReq()
        ..id = id
        ..force = force;

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

  /// 初始化上传任务
  /// 返回 upload_id 和已上传分片信息（支持断点续传）
  Future<ApiResult<InitUploadRespData>> initUpload({
    required String name,
    String? parentId,
    required int size,
    String? hash,
    String? mime,
    FileType? type,
    int? chunkSize,
  }) async {
    try {
      final req = InitUploadReq()
        ..name = name
        ..size = Int64(size);
      if (parentId != null) req.parentId = parentId;
      if (hash != null) req.hash = hash;
      if (mime != null) req.mime = mime;
      if (type != null) req.type = type;
      if (chunkSize != null) req.chunkSize = chunkSize;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/InitUpload',
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
  /// [chunkIndex] 分片序号（从1开始）
  /// [chunkData] 分片数据
  /// [chunkHash] 分片哈希（可选，用于校验）
  Future<ApiResult<UploadChunkRespData>> uploadChunk({
    required String uploadId,
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
        '$_baseUrl/native.FileService/UploadChunk',
        data: req.toProto3Json(),
        onSendProgress: onSendProgress,
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UploadChunkRespData>(
        data,
        UploadChunkRespData(),
      );
      _logger.info(
        '上传分片成功: uploadId=$uploadId, chunkIndex=$chunkIndex, progress=${respData.uploadedCount}/${respData.chunkTotal}',
      );
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('上传分片失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('上传分片失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 完成上传
  Future<ApiResult<CompleteUploadRespData>> completeUpload({
    required String uploadId,
  }) async {
    try {
      final req = CompleteUploadReq()..uploadId = uploadId;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/CompleteUpload',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<CompleteUploadRespData>(
        data,
        CompleteUploadRespData(),
      );
      _logger.info('完成上传成功: ${respData.fileId}');
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('完成上传失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('完成上传失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 获取上传进度
  Future<ApiResult<UploadProgressRespData>> getUploadProgress({
    required String uploadId,
  }) async {
    try {
      final req = UploadProgressReq()..uploadId = uploadId;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/GetUploadProgress',
        data: req.toProto3Json(),
      );

      final data = response.data as Map<String, dynamic>;
      final respData = extractRespData<UploadProgressRespData>(
        data,
        UploadProgressRespData(),
      );
      return ApiResult(success: true, data: respData);
    } on DioException catch (e) {
      _logger.error('获取上传进度失败: ${e.message}');
      return ApiResult(success: false, message: _handleDioError(e));
    } catch (e) {
      _logger.error('获取上传进度失败: $e');
      return ApiResult(success: false, message: e.toString());
    }
  }

  /// 取消上传
  Future<ApiResult<void>> cancelUpload({required String uploadId}) async {
    try {
      final req = CancelUploadReq()..uploadId = uploadId;

      final response = await _dio.post(
        '$_baseUrl/native.FileService/CancelUpload',
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
