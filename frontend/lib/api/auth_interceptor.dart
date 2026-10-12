import 'package:dio/dio.dart';

/// 构建统一的鉴权拦截器：注入 Bearer Token、记录请求 / 响应日志，并在登录态
/// 失效时回调 [onUnauthorized]。
///
/// 登录态失效必须收敛到同一条处理路径，而服务端有**两种**表现：
///   1. HTTP 401：`server/middleware_rest.go` 对 token 过期 / 缺失返回
///      `401 + BaseResp(TOKEN_INVALID)`。Dio 默认 validateStatus 只接受 2xx，
///      401 会先变成 [DioException]，响应体不会被解析，`extractRespData` 里那句
///      `throw TokenInvalidException` 永远不会执行；
///   2. HTTP 200 + `BaseResp.code == TOKEN_INVALID`：业务层判定 token 无效，
///      由 `extractRespData`（`api/util.dart`）抛 `TokenInvalidException`。
///
/// 本拦截器负责第 1 条。[onUnauthorized] 传 null 表示当前上下文无法跳转
/// （例如后台 isolate 里没有 GetIt 与路由），只把错误交给调用方。
InterceptorsWrapper buildAuthInterceptor({
  required String? Function() tokenProvider,
  required void Function(String message) logDebug,
  required void Function(String message) logError,
  void Function()? onUnauthorized,
}) {
  return InterceptorsWrapper(
    onRequest: (options, handler) {
      // 动态获取认证 Token，确保登录后能使用最新 token
      final accessToken = tokenProvider();
      if (accessToken != null && accessToken.isNotEmpty) {
        options.headers['Authorization'] = 'Bearer $accessToken';
      }
      logDebug('API Request: ${options.method} ${options.path}');
      return handler.next(options);
    },
    onResponse: (response, handler) {
      logDebug(
        'API Response: ${response.statusCode} ${response.requestOptions.path}',
      );
      return handler.next(response);
    },
    onError: (error, handler) {
      logError(
        'API Error: ${error.message} ${error.requestOptions.path}\n'
        'Status: ${error.response?.statusCode}\n'
        'Response: ${error.response?.data}',
      );
      if (onUnauthorized != null && isUnauthorizedError(error)) {
        onUnauthorized();
      }
      return handler.next(error);
    },
  );
}

/// 该错误是否代表**登录态失效**（需要清登录信息并回登录页）。
///
/// 排除 `/api/auth/login`：它的 401 是用户名或密码错误，与登录态无关
/// （见 README 的接口约定）。
bool isUnauthorizedError(DioException error) {
  if (error.requestOptions.path.endsWith('/api/auth/login')) return false;

  final response = error.response;
  if (response == null) return false;
  if (response.statusCode == 401) return true;

  // 其它非 2xx 状态码下，业务码 TOKEN_INVALID 同样算登录态失效
  final data = response.data;
  return data is Map && data['code'] == 'TOKEN_INVALID';
}
