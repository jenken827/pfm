import 'dart:ffi';
import 'dart:io' as io;
import 'dart:typed_data';

import 'package:ffi/ffi.dart';

/// 原生 BLAKE3 后端的加载与调用（FFI 加速，见 `DESIGN.md` §14.12.4）。
///
/// 这是**尽力而为**的加速层：库不存在（该平台/ABI 没构建）、符号缺失、
/// ABI 版本不匹配、空输入自检不通过，都会让 [instance] 为 null，调用方
/// 退回纯 Dart 实现。两条路径的摘要逐字节一致，功能不依赖原生库。
///
/// 加载顺序：
/// 1. 环境变量 `PFM_BLAKE3_LIB`（显式指定，测试与本机调试用）；
/// 2. 平台库名交给动态链接器按默认搜索路径找（Android 上即 APK 里的
///    `jniLibs/<abi>/libpfm_blake3.so`）；
/// 3. 可执行文件旁边的常规布局（`lib/`、同级目录、macOS 的 `Frameworks/`）。
final class Blake3Native {
  Blake3Native._(this._new, this._update, this._final, this._free);

  /// ABI 版本，必须与 `native/blake3_ffi/src/lib.rs` 的 `ABI_VERSION` 一致。
  static const int abiVersion = 1;

  /// 加载成功即为可用后端，否则为 null（退回纯 Dart）。
  static final Blake3Native? instance = _tryLoad();

  static const String _emptyDigestHex =
      'af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262';

  final Pointer<Void> Function() _new;
  final int Function(Pointer<Void>, Pointer<Uint8>, int) _update;
  final int Function(Pointer<Void>, Pointer<Uint8>, int) _final;
  final void Function(Pointer<Void>) _free;

  /// 新建一个哈希上下文；用完必须 [Blake3NativeHasher.dispose]。
  Blake3NativeHasher newHasher() => Blake3NativeHasher._(this);

  static Blake3Native? _tryLoad() {
    for (final candidate in _candidatePaths()) {
      try {
        final backend = _bind(DynamicLibrary.open(candidate));
        if (backend != null) {
          return backend;
        }
      } on Object {
        // 打不开 / 没符号：换下一个候选，全都失败就退回纯 Dart。
      }
    }
    return null;
  }

  static Blake3Native? _bind(DynamicLibrary lib) {
    final abi = lib.lookupFunction<Uint32 Function(), int Function()>(
      'pfm_blake3_abi_version',
    )();
    if (abi != abiVersion) {
      return null;
    }
    final backend = Blake3Native._(
      lib.lookupFunction<Pointer<Void> Function(), Pointer<Void> Function()>(
        'pfm_blake3_new',
      ),
      lib.lookupFunction<
        Int32 Function(Pointer<Void>, Pointer<Uint8>, Size),
        int Function(Pointer<Void>, Pointer<Uint8>, int)
      >('pfm_blake3_update'),
      lib.lookupFunction<
        Int32 Function(Pointer<Void>, Pointer<Uint8>, Size),
        int Function(Pointer<Void>, Pointer<Uint8>, int)
      >('pfm_blake3_final'),
      lib.lookupFunction<
        Void Function(Pointer<Void>),
        void Function(Pointer<Void>)
      >('pfm_blake3_free'),
    );

    // 自检：空输入摘要必须等于标准向量。加载到「别人家的同名库」或旧版
    // ABI 时，这里会拦住，而不是产出错误摘要把上传判成 hash mismatch。
    final probe = backend.newHasher();
    try {
      if (probe.finalizeHex() != _emptyDigestHex) {
        return null;
      }
    } on Object {
      return null;
    } finally {
      probe.dispose();
    }
    return backend;
  }

  static List<String> _candidatePaths() {
    final names = _libraryNames();
    if (names.isEmpty) {
      return const [];
    }
    final override = io.Platform.environment['PFM_BLAKE3_LIB'];
    String? exeDir;
    try {
      exeDir = io.File(io.Platform.resolvedExecutable).parent.path;
    } on Object {
      exeDir = null;
    }
    return <String>[
      if (override != null && override.isNotEmpty) override,
      ...names,
      if (exeDir != null) ...<String>[
        for (final name in names) '$exeDir/lib/$name',
        for (final name in names) '$exeDir/$name',
        for (final name in names) '$exeDir/../Frameworks/$name',
      ],
    ];
  }

  /// 当前平台的原生库文件名；未构建的平台返回空（直接退回纯 Dart）。
  static List<String> _libraryNames() {
    if (io.Platform.isAndroid || io.Platform.isLinux) {
      return const ['libpfm_blake3.so'];
    }
    if (io.Platform.isWindows) {
      return const ['pfm_blake3.dll'];
    }
    if (io.Platform.isMacOS) {
      return const ['libpfm_blake3.dylib'];
    }
    // iOS 需要静态链接进 App，当前没有对应产物；Web 不涉及（浏览器端是
    // server/webui，本身不支持上传）。
    return const [];
  }
}

/// 一块原生内存缓冲：`readInto` 直接往里写，再把指针交给 native `update`，
/// 不必把每块（默认 8 MiB）数据 memcpy 到 Dart 堆。
final class NativeBuffer {
  NativeBuffer(this.length) : _pointer = calloc<Uint8>(length);

  final int length;
  Pointer<Uint8> _pointer;

  /// 传给 FFI 的原始指针。
  Pointer<Uint8> get pointer => _pointer;

  /// 同一块内存的 `Uint8List` 视图（零拷贝）。
  Uint8List get bytes => _pointer.asTypedList(length);

  void dispose() {
    if (_pointer == nullptr) {
      return;
    }
    calloc.free(_pointer);
    _pointer = nullptr;
  }
}

/// 原生 BLAKE3 的增量哈希器（一个实例对应一个 `*mut Hasher`）。
///
/// 句柄只在创建它的 isolate 内使用；[FileHasher] 在 worker isolate 里串行调用。
final class Blake3NativeHasher {
  Blake3NativeHasher._raw(this._backend, this._handle);

  factory Blake3NativeHasher._(Blake3Native backend) {
    final handle = backend._new();
    if (handle == nullptr) {
      throw StateError('pfm_blake3_new returned null');
    }
    return Blake3NativeHasher._raw(backend, handle);
  }

  final Blake3Native _backend;
  Pointer<Void> _handle;
  bool _disposed = false;

  /// 追加 [length] 字节（零拷贝：直接读指针）。返回后调用方可复用该缓冲。
  void update(Pointer<Uint8> data, int length) {
    _ensureAlive();
    if (length == 0) {
      return;
    }
    final rc = _backend._update(_handle, data, length);
    if (rc != 0) {
      throw StateError('pfm_blake3_update failed with code $rc');
    }
  }

  /// 追加一小段内存数据（`prefix`、测试用）；会复制进原生内存。
  void updateBytes(List<int> data) {
    if (data.isEmpty) {
      return;
    }
    final buffer = calloc<Uint8>(data.length);
    try {
      buffer.asTypedList(data.length).setAll(0, data);
      update(buffer, data.length);
    } finally {
      calloc.free(buffer);
    }
  }

  /// 取 32 字节摘要（不释放句柄）。
  Uint8List finalize() {
    _ensureAlive();
    final out = calloc<Uint8>(32);
    try {
      final rc = _backend._final(_handle, out, 32);
      if (rc != 0) {
        throw StateError('pfm_blake3_final failed with code $rc');
      }
      return Uint8List.fromList(out.asTypedList(32));
    } finally {
      calloc.free(out);
    }
  }

  /// 32 字节摘要转小写 hex（64 字符，与后端 `hex.EncodeToString` 一致）。
  String finalizeHex() {
    return finalize().map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  /// 释放原生句柄；可重复调用。
  void dispose() {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _backend._free(_handle);
    _handle = nullptr;
  }

  void _ensureAlive() {
    if (_disposed) {
      throw StateError('Blake3NativeHasher already disposed');
    }
  }
}
