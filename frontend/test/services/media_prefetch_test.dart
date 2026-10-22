import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:get_it/get_it.dart';
import 'package:pfm/services/local_media_server.dart';
import 'package:pfm/services/media_prefetch.dart';
import 'package:talker_flutter/talker_flutter.dart';

/// 带 Range 支持的慢速测试服务器：每个 block 之间故意延迟，模拟弱网
class _SlowServer {
  _SlowServer(this.data);

  final Uint8List data;

  /// 每块之间的延迟：模拟弱网下的吞吐上限
  static const Duration blockDelay = Duration(milliseconds: 20);
  late HttpServer _server;
  int requestCount = 0;

  String get url => 'http://127.0.0.1:${_server.port}/video.mp4';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen(_handle);
  }

  Future<void> close() => _server.close(force: true);

  Future<void> _handle(HttpRequest request) async {
    requestCount += 1;
    final response = request.response;
    final String? header = request.headers.value(HttpHeaders.rangeHeader);
    var start = 0;
    var end = data.length - 1;
    if (header != null && header.startsWith('bytes=')) {
      final spec = header.substring(6).split(',').first.trim();
      final parts = spec.split('-');
      start = int.parse(parts[0]);
      end = parts[1].isEmpty ? end : min(int.parse(parts[1]), end);
      response.statusCode = HttpStatus.partialContent;
      response.headers.set(
        HttpHeaders.contentRangeHeader,
        'bytes $start-$end/${data.length}',
      );
    }
    response.headers.set(HttpHeaders.acceptRangesHeader, 'bytes');
    response.headers.contentLength = end - start + 1;
    const block = 32 << 10;
    for (var offset = start; offset <= end; offset += block) {
      final stop = min(offset + block, end + 1);
      response.add(data.sublist(offset, stop));
      await response.flush();
      await Future<void>.delayed(blockDelay);
    }
    await response.close();
  }
}

Uint8List _payload(int size) {
  final rng = Random(7);
  final bytes = Uint8List(size);
  for (var i = 0; i < size; i++) {
    bytes[i] = rng.nextInt(256);
  }
  return bytes;
}

void main() {
  late Directory tempDir;
  late Uint8List payload;
  late _SlowServer server;

  setUp(() async {
    // 预取器会写日志（getLogger 从 GetIt 取 Talker），测试里注册一个即可
    if (!GetIt.I.isRegistered<Talker>()) {
      GetIt.I.registerSingleton<Talker>(Talker());
    }
    tempDir = await Directory.systemTemp.createTemp('pfm_prefetch_test');
    payload = _payload(1024 * 1024); // 1MB
    server = _SlowServer(payload);
    await server.start();
  });

  tearDown(() async {
    await server.close();
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

  test('并行预取把整份文件落到本地，且内容与源一致', () async {
    final path = '${tempDir.path}/a.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 3,
      chunkSize: 256 << 10,
    );

    expect(prefetcher.size, payload.length);
    await prefetcher.ensure(payload.length - 1);

    expect(prefetcher.isComplete, isTrue);
    expect(prefetcher.progress, 1.0);
    expect(await File(path).length(), payload.length);
    expect(await File(path).readAsBytes(), equals(payload));
    expect(File('$path.done').existsSync(), isTrue);
    // 分块并行：请求数应明显多于 1（探测 1 次 + 各块）
    expect(server.requestCount, greaterThan(2));

    await prefetcher.dispose();
  });

  test('ensure 会等到数据就绪，不会提前返回', () async {
    final path = '${tempDir.path}/b.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 1,
      chunkSize: 128 << 10,
    );

    var done = false;
    final last = payload.length - 1;
    unawaited(prefetcher.ensure(last).then((_) => done = true));
    await Future<void>.delayed(const Duration(milliseconds: 60));
    expect(done, isFalse, reason: '尾部还没下到，ensure 不应完成');

    await prefetcher.ensure(last);
    expect(done, isTrue);
    await prefetcher.dispose();
  });

  test('缓存命中：第二次打开不再发起请求', () async {
    final path = '${tempDir.path}/c.mp4';
    final first = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 2,
      chunkSize: 512 << 10,
    );
    await first.ensure(payload.length - 1);
    await first.dispose();

    final before = server.requestCount;
    final second = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 2,
    );
    expect(second.isComplete, isTrue);
    expect(second.progress, 1.0);
    // 只有探测请求（1 次），没有重新下载
    expect(server.requestCount - before, 1);
    await second.dispose();
  });

  test('本地媒体服务：整段请求返回完整字节', () async {
    final path = '${tempDir.path}/d.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 4,
      chunkSize: 256 << 10,
    );
    final local = await LocalMediaServer.start(prefetcher);
    addTearDown(() async {
      await local.close();
      await prefetcher.dispose();
    });

    final client = HttpClient();
    final request = await client.getUrl(Uri.parse(local.url));
    final response = await request.close();
    expect(response.statusCode, HttpStatus.ok);
    expect(response.headers.contentLength, payload.length);

    final received = <int>[];
    await for (final chunk in response) {
      received.addAll(chunk);
    }
    client.close(force: true);

    expect(received.length, payload.length);
    expect(received, equals(payload));
  });

  test('本地媒体服务：Range 请求返回 206 与正确的切片', () async {
    final path = '${tempDir.path}/e.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 4,
      chunkSize: 256 << 10,
    );
    final local = await LocalMediaServer.start(prefetcher);
    addTearDown(() async {
      await local.close();
      await prefetcher.dispose();
    });

    const start = 1000;
    const end = 5000;
    final client = HttpClient();
    final request = await client.getUrl(Uri.parse(local.url));
    request.headers.set(HttpHeaders.rangeHeader, 'bytes=$start-$end');
    final response = await request.close();
    expect(response.statusCode, HttpStatus.partialContent);
    expect(
      response.headers.value(HttpHeaders.contentRangeHeader),
      'bytes $start-$end/${payload.length}',
    );

    final received = <int>[];
    await for (final chunk in response) {
      received.addAll(chunk);
    }
    client.close(force: true);

    expect(received, equals(payload.sublist(start, end + 1)));
  });

  test('本地媒体服务：请求尚未下载到的区域会等待而不是报错', () async {
    final path = '${tempDir.path}/f.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 1,
      chunkSize: 128 << 10,
    );
    final local = await LocalMediaServer.start(prefetcher);
    addTearDown(() async {
      await local.close();
      await prefetcher.dispose();
    });

    // 直接要尾部：此时还没下到，服务应当阻塞到数据就绪
    final client = HttpClient();
    final request = await client.getUrl(Uri.parse(local.url));
    request.headers.set(
      HttpHeaders.rangeHeader,
      'bytes=${payload.length - 200}-${payload.length - 1}',
    );
    final response = await request.close();
    expect(response.statusCode, HttpStatus.partialContent);

    final received = <int>[];
    await for (final chunk in response) {
      received.addAll(chunk);
    }
    client.close(force: true);

    expect(received, equals(payload.sublist(payload.length - 200)));
  });

  test('取消预取：dispose 后不再抛未捕获异常', () async {
    final path = '${tempDir.path}/g.mp4';
    final prefetcher = await MediaPrefetcher.open(
      url: server.url,
      cachePath: path,
      connections: 2,
      chunkSize: 128 << 10,
    );
    await Future<void>.delayed(const Duration(milliseconds: 40));
    await prefetcher.dispose();
    expect(prefetcher.isComplete, isFalse);
  });
}
