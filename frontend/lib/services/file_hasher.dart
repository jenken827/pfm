import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;
import 'dart:isolate';
import 'dart:math';
import 'dart:typed_data';

// 直接导入 blake3_dart 内部类以支持增量哈希（该包只暴露一次性函数）。
// ignore: implementation_imports
import 'package:blake3_dart/src/blake3_compact.dart';
import 'package:pfm/services/blake3_native.dart';

/// 文件哈希计算失败（文件不存在 / 读不动 / worker isolate 内异常）。
class FileHashException implements Exception {
  FileHashException(this.message);

  final String message;

  @override
  String toString() => 'FileHashException: $message';
}

/// 大文件 BLAKE3（32 字节摘要，64 字符小写 hex）。
///
/// 全文哈希一律在 worker isolate 里读文件、算哈希，主 isolate 只收进度与结果，
/// 避免哈希把 UI 拖住。后端按可用性选择（见 `DESIGN.md` §14.12）：
///
/// * 优先原生实现（Rust + SIMD/rayon，见 `blake3_native.dart`）；
/// * 没有当前平台的原生库（iOS / macOS / Windows / Web）或加载失败时，
///   退回 `blake3_dart` 纯 Dart 实现。
///
/// 两条路径摘要必须逐字节一致：32 字节输出、不截断；带 [prefix] 时按
/// `prefix 字节 + 文件字节` 的顺序喂同一个 hasher（与改前 `HashContext`
/// 逐块 update 的语义相同），并与后端 `blake3.New(32, nil)` 一致。
class FileHasher {
  FileHasher._();

  /// 单次读块大小：8 MiB 在进度粒度与 isolate 消息开销之间折中。
  static const int defaultBlockSize = 8 << 20;

  /// 计算 [path] 的内容哈希。
  ///
  /// [onProgress] 在主 isolate 回调，参数为（已处理字节, 总字节）；打开文件后
  /// 先回调一次 `(0, total)`，之后每读满一块回调一次。总字节取打开时的文件
  /// 长度，读取过程中文件被截短时按实际读到的字节收尾（进度不再前进）。
  ///
  /// 失败抛 [FileHashException]。
  static Future<String> hashFile(
    String path, {
    String prefix = '',
    void Function(int hashed, int total)? onProgress,
    int blockSize = defaultBlockSize,
  }) async {
    if (blockSize <= 0) {
      throw FileHashException('blockSize must be positive: $blockSize');
    }
    final replyPort = ReceivePort();
    Isolate? isolate;
    StreamSubscription<dynamic>? subscription;
    try {
      isolate = await Isolate.spawn<_HashRequest>(
        _hashFileWorker,
        _HashRequest(
          path: path,
          prefix: prefix,
          blockSize: blockSize,
          reply: replyPort.sendPort,
        ),
        debugName: 'blake3-hash',
      );
      final completer = Completer<String>();
      subscription = replyPort.listen((dynamic message) {
        if (message is _ProgressMessage) {
          onProgress?.call(message.hashed, message.total);
        } else if (message is _DoneMessage) {
          if (!completer.isCompleted) {
            completer.complete(message.hex);
          }
        } else if (message is _ErrorMessage) {
          if (!completer.isCompleted) {
            completer.completeError(FileHashException(message.message));
          }
        }
      });
      return await completer.future;
    } finally {
      await subscription?.cancel();
      replyPort.close();
      // 正常结束的 worker 已经退出，这里 kill 只是兜底（异常/提前返回时）。
      isolate?.kill(priority: Isolate.immediate);
    }
  }

  /// 内存里的小数据哈希（分片哈希、备份内容哈希）。
  ///
  /// 分片只有 2 MiB 左右，走 isolate 往返（消息拷贝 + 调度）比直接算更贵，
  /// 因此留在调用方 isolate 内联计算，见 `DESIGN.md` §14.12.3。
  static String hashBytes(List<int> data) {
    return blake3Hex(Uint8List.fromList(data));
  }

  /// 把 32 字节摘要转成小写 hex。
  static String _toHex(Uint8List digest) {
    return digest.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }
}

/// worker 入口：读文件、算哈希，把进度/结果/错误发回主 isolate。
void _hashFileWorker(_HashRequest request) {
  _runHashFile(request).then(
    (hex) => request.reply.send(_DoneMessage(hex)),
    onError: (Object error) => request.reply.send(_ErrorMessage('$error')),
  );
}

Future<String> _runHashFile(_HashRequest request) async {
  final native = Blake3Native.instance;
  if (native != null) {
    try {
      return await _runHashFileNative(request, native);
    } on io.FileSystemException {
      // 文件本身的问题（不存在 / 读失败）与后端无关，别再拿纯 Dart 重读一遍
      rethrow;
    } on Object {
      // 原生栈异常（FFI 调用失败 / 分配失败 / 该平台库不兼容）→ 从头用纯 Dart
      // 重算，宁慢勿错。进度会从 0 重新发一遍，调用方取最后一帧即可。
    }
  }
  return _runHashFileInDart(request);
}

/// 原生后端（SIMD + rayon），吞吐比纯 Dart 高一到两个数量级。
///
/// 读块走「原生内存视图」：`calloc` 出来的缓冲直接交给 `readInto`，再把指针
/// 交给 native `update`，全程零拷贝（8 MiB 的 memcpy 在手机上也要几毫秒）。
Future<String> _runHashFileNative(
  _HashRequest request,
  Blake3Native backend,
) async {
  final hasher = backend.newHasher();
  final buffer = NativeBuffer(request.blockSize);
  final raf = await io.File(request.path).open();
  try {
    final total = await raf.length();
    if (request.prefix.isNotEmpty) {
      hasher.updateBytes(utf8.encode(request.prefix));
    }
    request.reply.send(_ProgressMessage(0, total));

    final view = buffer.bytes;
    var hashed = 0;
    while (hashed < total) {
      final want = min(request.blockSize, total - hashed);
      final target = want == request.blockSize
          ? view
          : Uint8List.sublistView(view, 0, want);
      final read = await raf.readInto(target);
      if (read <= 0) {
        // 文件在读取过程中被截短：按已读到的内容收尾。
        break;
      }
      hasher.update(buffer.pointer, read);
      hashed += read;
      request.reply.send(_ProgressMessage(hashed, total));
    }
    return hasher.finalizeHex();
  } finally {
    await raf.close();
    buffer.dispose();
    hasher.dispose();
  }
}

/// 纯 Dart 后端：没有原生库的平台（iOS / macOS / Windows / Web）或加载失败时用。
Future<String> _runHashFileInDart(_HashRequest request) async {
  final raf = await io.File(request.path).open();
  try {
    final total = await raf.length();
    final hasher = HashContext.unkeyed();
    if (request.prefix.isNotEmpty) {
      hasher.update(Uint8List.fromList(utf8.encode(request.prefix)));
    }
    request.reply.send(_ProgressMessage(0, total));

    var hashed = 0;
    while (hashed < total) {
      final want = min(request.blockSize, total - hashed);
      final block = await raf.read(want);
      if (block.isEmpty) {
        // 文件在读取过程中被截短：按已读到的内容收尾，不再发进度。
        break;
      }
      hasher.update(block);
      hashed += block.length;
      request.reply.send(_ProgressMessage(hashed, total));
    }

    final digest = Uint8List(32);
    hasher.finalize(digest);
    return FileHasher._toHex(digest);
  } finally {
    await raf.close();
  }
}

/// spawn 时传给 worker 的入参（仅含可跨 isolate 传递的值与 SendPort）。
class _HashRequest {
  const _HashRequest({
    required this.path,
    required this.prefix,
    required this.blockSize,
    required this.reply,
  });

  final String path;
  final String prefix;
  final int blockSize;
  final SendPort reply;
}

class _ProgressMessage {
  const _ProgressMessage(this.hashed, this.total);

  final int hashed;
  final int total;
}

class _DoneMessage {
  const _DoneMessage(this.hex);

  final String hex;
}

class _ErrorMessage {
  const _ErrorMessage(this.message);

  final String message;
}
