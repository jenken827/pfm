import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:pfm/services/logger.dart';

/// 并行分片预取器：把远端媒体按 [chunkSize] 切块，用 [connections] 条连接并发拉取，
/// 写入本地缓存文件；配合 `LocalMediaServer` 实现「边下边播」。
///
/// 为什么不用单一连接直接边下边播：实测链路只有 7Mbps 左右且会出现几秒的断流窗口，
/// 播放器直接读远端时缓冲区不断被打空。预取把「网络」和「播放」解耦——播放器只读
/// 127.0.0.1，永远毫秒级拿到数据，卡顿与链路抖动解耦。
///
/// 关键约定：
/// - 文件按 `truncate(size)` 预分配，块状态只存在内存里；进程重启后重下（缓存完成会写
///   `<path>.done` 标记，重启后直接命中）。
/// - 文件读写用同步 API + 独立 RandomAccessFile：同步调用天然原子，避免多 worker 与
///   本地服务器交错 `setPosition` / `writeFrom` 把数据写串。
class MediaPrefetcher {
  /// 参数顺序：uri / 缓存文件 / 句柄 / 总长 / 块大小 / 连接数 / 额外请求头 / 是否已完成
  MediaPrefetcher._(
    this._uri,
    this._file,
    this._raf,
    this.size,
    this._chunkSize,
    this.connections,
    this.requestHeaders,
    bool complete,
  ) : _complete = complete {
    _chunkCount = (size + _chunkSize - 1) ~/ _chunkSize;
    _chunkDone = List<bool>.filled(_chunkCount, complete);
  }

  final Uri _uri;
  final File _file;
  final RandomAccessFile _raf;
  final int _chunkSize;

  /// 并发连接数
  final int connections;

  /// 额外的请求头（票据在 URL 里时不传也行）
  final Map<String, String> requestHeaders;

  /// 文件总字节数（由服务器 Content-Range 得到）
  final int size;

  bool _complete;
  bool _cancelled = false;
  Object? _error;

  late final int _chunkCount;
  late final List<bool> _chunkDone;
  int _nextChunk = 0;
  late int _contiguousIndex = _complete ? _chunkCount : 0;
  int _bytesDone = 0;

  final List<_Waiter> _waiters = [];
  final List<Future<void>> _workers = [];
  final List<HttpClient> _clients = [];

  int _sampleBytes = 0;
  DateTime _sampleAt = DateTime.now();
  double _bytesPerSecond = 0;

  /// 本地缓存文件（完整的 MP4 内容，未下载完的区域是零填充）
  File get file => _file;

  /// 已下载字节数
  int get bytesDone => _complete ? size : _bytesDone;

  /// 0.0 ~ 1.0
  double get progress => _complete ? 1 : (size <= 0 ? 0 : bytesDone / size);

  /// 最近测得的聚合下载速率（字节/秒）
  double get bytesPerSecond => _bytesPerSecond;

  /// 是否整份都已落到本地
  bool get isComplete => _complete;

  /// 从 0 开始连续可读的字节数（本地服务器只能安全读到这里）
  int get contiguousBytes {
    if (_complete) return size;
    return min(size, _contiguousIndex * _chunkSize);
  }

  /// 开始预取：先探测文件大小，再拉起并发 worker。
  ///
  /// [cachePath] 是缓存文件路径；同目录下 `<cachePath>.done` 存在且长度匹配时直接命中。
  static Future<MediaPrefetcher> open({
    required String url,
    required String cachePath,
    Map<String, String> headers = const {},
    int connections = 6,
    int chunkSize = 1 << 20,
  }) async {
    final Uri uri = Uri.parse(url);
    final File file = File(cachePath);
    final File marker = File('$cachePath.done');

    final int size = await _probeSize(uri, headers);
    if (size <= 0) {
      throw const FormatException('无法确定远端文件大小（服务器未返回 Content-Range）');
    }

    final bool sizeMatches = await file.exists() && await file.length() == size;
    final bool complete = sizeMatches && await marker.exists();

    await file.parent.create(recursive: true);
    final RandomAccessFile raf = await file.open(mode: FileMode.write);
    if (!complete) {
      // 预分配到最终长度：后续写入只改内容，长度不再变化
      await raf.truncate(size);
    }

    final prefetcher = MediaPrefetcher._(
      uri,
      file,
      raf,
      size,
      chunkSize,
      connections,
      headers,
      complete,
    );

    if (complete) {
      getLogger().info('视频预取命中缓存: $cachePath ($size 字节)');
      prefetcher._bytesDone = size;
    } else {
      getLogger().info(
        '视频预取开始: $cachePath, $size 字节, ${prefetcher._chunkCount} 块, '
        '$_describeConnections(connections)',
      );
      prefetcher._startWorkers();
    }
    return prefetcher;
  }

  static String _describeConnections(int n) => '$n 条连接并发';

  /// `Range: bytes=0-0` 探测总长度：响应头里 `Content-Range: bytes 0-0/<total>`。
  static Future<int> _probeSize(Uri uri, Map<String, String> headers) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 15);
    try {
      final request = await client.getUrl(uri);
      headers.forEach(request.headers.set);
      request.headers.set(HttpHeaders.rangeHeader, 'bytes=0-0');
      final response = await request.close();
      final contentRange = response.headers.value(HttpHeaders.contentRangeHeader);
      await response.drain<void>();
      if (response.statusCode != HttpStatus.partialContent) {
        throw HttpException(
          '服务器不支持 Range（HTTP ${response.statusCode}），无法并行预取',
          uri: uri,
        );
      }
      if (contentRange == null || !contentRange.contains('/')) {
        throw HttpException('缺少 Content-Range 响应头', uri: uri);
      }
      return int.parse(contentRange.split('/').last.trim());
    } finally {
      client.close(force: true);
    }
  }

  void _startWorkers() {
    for (var i = 0; i < connections; i++) {
      final client = HttpClient()..connectionTimeout = const Duration(seconds: 15);
      _clients.add(client);
      _workers.add(_worker(client));
    }
  }

  Future<void> _worker(HttpClient client) async {
    while (!_cancelled && _error == null) {
      final int index = _nextChunk++;
      if (index >= _chunkCount) return;
      if (_chunkDone[index]) continue;
      final int start = index * _chunkSize;
      final int end = min(size, start + _chunkSize) - 1;
      try {
        await _fetchChunk(client, index, start, end);
      } catch (e) {
        if (_cancelled) return;
        _fail(e);
        return;
      }
    }
  }

  /// 拉取一个块并落盘；只有整块到齐才标记完成，保证 contiguousBytes 语义
  Future<void> _fetchChunk(HttpClient client, int index, int start, int end) async {
    final request = await client.getUrl(_uri);
    requestHeaders.forEach(request.headers.set);
    request.headers.set(HttpHeaders.rangeHeader, 'bytes=$start-$end');
    final response = await request.close();
    if (response.statusCode != HttpStatus.partialContent) {
      await response.drain<void>();
      throw HttpException('分片请求期望 206，实际 ${response.statusCode}');
    }

    var offset = start;
    await for (final data in response) {
      if (_cancelled || _error != null) return;
      _raf.setPositionSync(offset);
      _raf.writeFromSync(data);
      offset += data.length;
      _bytesDone += data.length;
      _sampleSpeed();
    }

    if (offset != end + 1) {
      throw HttpException('分片 $index 未收全：期望 ${end - start + 1} 字节，实际 ${offset - start}');
    }
    _chunkDone[index] = true;
    _advanceContiguous();
  }

  void _sampleSpeed() {
    final now = DateTime.now();
    final int ms = now.difference(_sampleAt).inMilliseconds;
    if (ms < 500) return;
    final int delta = _bytesDone - _sampleBytes;
    _bytesPerSecond = delta * 1000 / ms;
    _sampleBytes = _bytesDone;
    _sampleAt = now;
  }

  void _advanceContiguous() {
    while (_contiguousIndex < _chunkCount && _chunkDone[_contiguousIndex]) {
      _contiguousIndex++;
    }
    _notifyWaiters();
    if (_contiguousIndex >= _chunkCount) _finish();
  }

  void _finish() {
    if (_complete) return;
    _complete = true;
    _bytesDone = size;
    try {
      File('${_file.path}.done').writeAsStringSync('$size');
    } catch (e) {
      getLogger().warning('写预取完成标记失败: $e');
    }
    getLogger().info('视频预取完成: ${_file.path} ($size 字节)');
    _notifyWaiters();
  }

  void _notifyWaiters() {
    if (_waiters.isEmpty) return;
    final int ready = contiguousBytes;
    final List<_Waiter> keep = [];
    for (final waiter in _waiters) {
      if (waiter.offset < ready) {
        waiter.completer.complete();
      } else {
        keep.add(waiter);
      }
    }
    _waiters
      ..clear()
      ..addAll(keep);
  }

  void _fail(Object error) {
    if (_error != null || _cancelled) return;
    _error = error;
    getLogger().error('视频预取失败: $error');
    for (final waiter in _waiters) {
      waiter.completer.completeError(error);
    }
    _waiters.clear();
  }

  /// 等待到 [offset] 处可读；本地服务器每读一段前都调一次
  Future<void> ensure(int offset) {
    if (_error != null) return Future.error(_error!);
    if (_complete || offset < contiguousBytes) return Future.value();
    final waiter = _Waiter(offset, Completer<void>());
    _waiters.add(waiter);
    return waiter.completer.future;
  }

  /// 从缓存文件读 [length] 字节（调用方保证 offset 已 ensure 过）
  List<int> read(int offset, int length) {
    _raf.setPositionSync(offset);
    return _raf.readSync(length);
  }

  /// 停掉 worker、关闭文件句柄。缓存文件保留（下次命中或重下）
  Future<void> dispose() async {
    if (_cancelled) return;
    _cancelled = true;
    for (final client in _clients) {
      client.close(force: true);
    }
    for (final waiter in _waiters) {
      if (!waiter.completer.isCompleted) {
        waiter.completer.completeError(const HttpException('预取已取消'));
      }
    }
    _waiters.clear();
    await Future.wait(_workers).catchError((_) => <void>[]);
    try {
      await _raf.close();
    } catch (_) {
      // 已关闭
    }
  }
}

class _Waiter {
  _Waiter(this.offset, this.completer);

  final int offset;
  final Completer<void> completer;
}
