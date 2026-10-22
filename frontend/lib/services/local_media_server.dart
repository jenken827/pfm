import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:pfm/services/logger.dart';
import 'package:pfm/services/media_prefetch.dart';

/// 本地媒体服务器：把「预取中的缓存文件」当成本机视频源暴露给播放器。
///
/// 播放器请求某段字节时，如果预取还没到那里，就在这里等（不返回错误、不返回空数据），
/// 等到了立即写出。于是播放器看到的是一个「永不缺数据、可 seek」的本机文件，
/// 网络抖动 / 断流全部由预取层吸收。
///
/// 只监听回环地址，且只服务一个文件（路径固定 `/media`）。
class LocalMediaServer {
  LocalMediaServer._(this._server, this._prefetcher);

  final HttpServer _server;
  final MediaPrefetcher _prefetcher;

  /// 播放器使用的本地 URL
  String get url => 'http://127.0.0.1:${_server.port}/media';

  /// 每块读写的字节数（太小系统调用多，太大首包变慢）
  static const int blockSize = 64 << 10;

  static Future<LocalMediaServer> start(MediaPrefetcher prefetcher) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final local = LocalMediaServer._(server, prefetcher);
    server.listen(
      local._handle,
      onError: (Object e) => getLogger().warning('本地媒体服务出错: $e'),
    );
    getLogger().info('本地媒体服务已启动: ${local.url}（源 ${prefetcher.size} 字节）');
    return local;
  }

  Future<void> close() async {
    await _server.close(force: true);
  }

  Future<void> _handle(HttpRequest request) async {
    final response = request.response;
    try {
      final int size = _prefetcher.size;
      final (int start, int end) = _parseRange(request, size);
      _writeHeaders(response, size, start, end);
      await _streamBody(response, start, end);
    } catch (e) {
      // 客户端（播放器）主动断开时写入会抛异常，属正常，别当错误刷日志
      if (e is! HttpException && e is! SocketException) {
        getLogger().warning('本地媒体服务写响应失败: $e');
      }
    } finally {
      try {
        await response.close();
      } catch (_) {
        // 已断开
      }
    }
  }

  /// 解析 `Range: bytes=start-end` / `bytes=start-` / `bytes=-suffix`
  (int, int) _parseRange(HttpRequest request, int size) {
    final String? header = request.headers.value(HttpHeaders.rangeHeader);
    if (header == null || !header.startsWith('bytes=')) {
      return (0, size - 1);
    }
    final String spec = header.substring('bytes='.length).split(',').first.trim();
    final int dash = spec.indexOf('-');
    if (dash < 0) return (0, size - 1);
    final String head = spec.substring(0, dash).trim();
    final String tail = spec.substring(dash + 1).trim();
    if (head.isEmpty) {
      // 后缀形式：最后 N 字节
      final int suffix = int.tryParse(tail) ?? 0;
      if (suffix <= 0) return (0, size - 1);
      return (max(0, size - suffix), size - 1);
    }
    final int start = int.parse(head);
    final int end = tail.isEmpty ? size - 1 : min(int.parse(tail), size - 1);
    return (max(0, min(start, size - 1)), max(0, end));
  }

  void _writeHeaders(HttpResponse response, int size, int start, int end) {
    final bool partial = !(start == 0 && end == size - 1);
    response.statusCode = partial ? HttpStatus.partialContent : HttpStatus.ok;
    response.headers.set(HttpHeaders.acceptRangesHeader, 'bytes');
    response.headers.set(HttpHeaders.contentTypeHeader, 'video/mp4');
    response.headers.contentLength = end - start + 1;
    if (partial) {
      response.headers.set(
        HttpHeaders.contentRangeHeader,
        'bytes $start-$end/$size',
      );
    }
  }

  /// 从预取文件按块搬运到响应；每块先等预取把该位置准备好
  Future<void> _streamBody(HttpResponse response, int start, int end) async {
    var offset = start;
    while (offset <= end) {
      await _prefetcher.ensure(offset);
      final int want = min(blockSize, end - offset + 1);
      final List<int> bytes = _prefetcher.read(offset, want);
      if (bytes.isEmpty) break;
      response.add(bytes);
      await response.flush();
      offset += bytes.length;
    }
  }
}
