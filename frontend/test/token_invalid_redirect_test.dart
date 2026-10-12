import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/api/auth_interceptor.dart';

/// 固定应答的假适配器：不发真实网络请求
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.statusCode, this.body);

  final int statusCode;
  final Object? body;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      jsonEncode(body),
      statusCode,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  // 契约：服务端 token 过期走 HTTP 401（server/middleware_rest.go），
  // Dio 默认 validateStatus 只接受 2xx，所以 401 到这里是 DioException，
  // 必须被判定为登录态失效——否则只会提示文案、不跳登录页。
  group('isUnauthorizedError', () {
    DioException badResponse(
      int statusCode, {
      String path = 'http://host/api/space/get',
      Object? data,
    }) {
      final options = RequestOptions(path: path);
      return DioException(
        requestOptions: options,
        type: DioExceptionType.badResponse,
        response: Response<dynamic>(
          requestOptions: options,
          statusCode: statusCode,
          data: data,
        ),
      );
    }

    test('HTTP 401 判定为登录态失效', () {
      expect(
        isUnauthorizedError(badResponse(401, data: {'code': 'TOKEN_INVALID'})),
        isTrue,
      );
    });

    test('其它状态码 + 业务码 TOKEN_INVALID 同样判定为登录态失效', () {
      expect(
        isUnauthorizedError(badResponse(400, data: {'code': 'TOKEN_INVALID'})),
        isTrue,
      );
    });

    test('403 无 TOKEN_INVALID 不是登录态失效', () {
      expect(
        isUnauthorizedError(
          badResponse(403, data: {'code': 'PERMISSION_DENIED'}),
        ),
        isFalse,
      );
    });

    test('登录接口的 401 是凭据错误，不是登录态失效', () {
      expect(
        isUnauthorizedError(
          badResponse(
            401,
            path: 'http://host/api/auth/login',
            data: '用户名或密码错误',
          ),
        ),
        isFalse,
      );
    });

    test('无响应的网络错误不是登录态失效', () {
      expect(
        isUnauthorizedError(
          DioException(
            requestOptions: RequestOptions(path: 'http://host/api/space/get'),
            type: DioExceptionType.connectionError,
          ),
        ),
        isFalse,
      );
    });
  });

  group('buildAuthInterceptor', () {
    late int unauthorizedCount;

    Dio buildDio(_FakeAdapter adapter, {required String path}) {
      final dio = Dio(BaseOptions(baseUrl: 'http://host'))
        ..httpClientAdapter = adapter
        ..interceptors.add(
          buildAuthInterceptor(
            tokenProvider: () => 'expired-token',
            logDebug: (_) {},
            logError: (_) {},
            onUnauthorized: () => unauthorizedCount++,
          ),
        );
      return dio;
    }

    setUp(() => unauthorizedCount = 0);

    test('401 触发 onUnauthorized（回归点：此前只提示不跳转）', () async {
      final dio = buildDio(
        _FakeAdapter(401, {'code': 'TOKEN_INVALID', 'message': 'no token'}),
        path: '/api/space/get',
      );

      await expectLater(
        dio.post('/api/space/get', data: <String, dynamic>{}),
        throwsA(isA<DioException>()),
      );
      expect(unauthorizedCount, 1);
    });

    test('登录接口 401 不触发 onUnauthorized', () async {
      final dio = buildDio(
        _FakeAdapter(401, '用户名或密码错误'),
        path: '/api/auth/login',
      );

      await expectLater(
        dio.post('/api/auth/login', data: <String, dynamic>{}),
        throwsA(isA<DioException>()),
      );
      expect(unauthorizedCount, 0);
    });

    test('成功响应不触发 onUnauthorized', () async {
      final dio = buildDio(
        _FakeAdapter(200, {'code': 'SUCCESS'}),
        path: '/api/space/get',
      );

      await dio.post('/api/space/get', data: <String, dynamic>{});
      expect(unauthorizedCount, 0);
    });
  });
}
