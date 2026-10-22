/// 视频「流畅播放」（服务端低码率转码）的客户端状态机。
///
/// 服务端 [GET /api/file/stream/transcode] 的语义：
/// - 200 / 206：转码副本已就绪，可以交给播放器；
/// - 202：还在转码或排队，body 里带 `progress`（0-100），稍后重试；
/// - 501：服务端没有 ffmpeg，功能未启用；
/// - 401 / 403 / 404：票据 / 权限 / 文件有问题，重试没有意义。
///
/// 这里只做「状态码 → 状态」的映射与轮询，不碰 Dio / UI，便于单测。
library;

/// 转码状态
enum TranscodeState {
  /// 已就绪（200/206）
  ready,

  /// 正在转码（202 + state=running）
  running,

  /// 排队中（202 + state=queued）
  queued,

  /// 服务端未启用转码（501）
  disabled,

  /// 请求被拒绝或出错（4xx 等）
  failed,

  /// 网络异常（拿不到响应）
  networkError,

  /// 轮询次数用尽
  timeout,

  /// 调用方取消
  cancelled,
}

/// 一次探测的结果
class TranscodeProbeResult {
  const TranscodeProbeResult({
    required this.state,
    this.progress = 0,
    this.statusCode = 0,
  });

  final TranscodeState state;

  /// 转码进度 0-100（仅 running / queued 有意义）
  final int progress;

  /// 原始 HTTP 状态码（网络异常为 0）
  final int statusCode;

  /// 是否还需要继续轮询
  bool get shouldContinue =>
      state == TranscodeState.running ||
      state == TranscodeState.queued ||
      state == TranscodeState.networkError;

  /// 把 HTTP 状态码与 202 的 body 映射成结果
  static TranscodeProbeResult fromResponse(
    int statusCode,
    Map<String, dynamic>? body,
  ) {
    switch (statusCode) {
      case 200:
      case 206:
        return TranscodeProbeResult(
          state: TranscodeState.ready,
          progress: 100,
          statusCode: statusCode,
        );
      case 202:
        final rawState = body?['state'];
        final rawProgress = body?['progress'];
        final progress = rawProgress is num ? rawProgress.toInt() : 0;
        return TranscodeProbeResult(
          state: rawState == 'queued'
              ? TranscodeState.queued
              : TranscodeState.running,
          progress: progress.clamp(0, 100),
          statusCode: statusCode,
        );
      case 501:
        return const TranscodeProbeResult(
          state: TranscodeState.disabled,
          statusCode: 501,
        );
      case 0:
        return const TranscodeProbeResult(state: TranscodeState.networkError);
      default:
        return TranscodeProbeResult(
          state: TranscodeState.failed,
          statusCode: statusCode,
        );
    }
  }
}

/// 轮询直到转码就绪。
///
/// - [probe] 每次探测一次（通常是 GET + `Range: bytes=0-0`）；
/// - [interval] 探测间隔，服务端 `Retry-After` 是 3 秒；
/// - [maxAttempts] 上限，避免弱网下无限等待；
/// - [onUpdate] 每拿到一次结果回调（用来刷新进度文案）；
/// - [isCancelled] 页面已销毁时提前退出；
/// - 连续 [maxNetworkErrors] 次网络异常就放弃，别把"断网"当成"还在转码"。
Future<TranscodeProbeResult> pollTranscode({
  required Future<TranscodeProbeResult> Function() probe,
  Duration interval = const Duration(seconds: 3),
  int maxAttempts = 200,
  int maxNetworkErrors = 3,
  void Function(TranscodeProbeResult result)? onUpdate,
  bool Function()? isCancelled,
}) async {
  var networkErrors = 0;
  for (var attempt = 0; attempt < maxAttempts; attempt++) {
    if (isCancelled?.call() ?? false) {
      return const TranscodeProbeResult(state: TranscodeState.cancelled);
    }

    TranscodeProbeResult result;
    try {
      result = await probe();
    } catch (_) {
      result = const TranscodeProbeResult(state: TranscodeState.networkError);
    }
    onUpdate?.call(result);
    if (result.state == TranscodeState.ready) return result;

    if (result.state == TranscodeState.networkError) {
      networkErrors++;
      if (networkErrors >= maxNetworkErrors) return result;
    } else {
      networkErrors = 0;
      if (!result.shouldContinue) return result;
    }
    await Future<void>.delayed(interval);
  }
  return const TranscodeProbeResult(state: TranscodeState.timeout);
}
