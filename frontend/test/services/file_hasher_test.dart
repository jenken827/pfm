import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/blake3_native.dart';
import 'package:pfm/services/file_hasher.dart';

/// `FileHasher` 的口径与 isolate 行为单测。
///
/// 这些用例把「与后端 `blake3.New(32, nil)` 一致」钉死：摘要必须和内存里
/// 一次性算的 BLAKE3 逐字节相同，否则上传会被 `CompleteUpload` 的
/// `finalHash != record.FileHash` 判失败（见 DESIGN.md §14.12）。
void main() {
  late Directory tmp;

  setUp(() {
    tmp = Directory.systemTemp.createTempSync('pfm_file_hasher_test');
  });

  tearDown(() {
    if (tmp.existsSync()) {
      tmp.deleteSync(recursive: true);
    }
  });

  File writeFile(String name, List<int> bytes) {
    final file = File('${tmp.path}/$name');
    file.writeAsBytesSync(bytes);
    return file;
  }

  /// 跨块的对拍数据：每块内容不同，避免"重复块恰好算对"的假绿。
  Uint8List sampleBytes(int length, {int seed = 7}) {
    return Uint8List.fromList(
      List<int>.generate(length, (i) => (i * seed + (i >> 8)) & 0xff),
    );
  }

  group('FileHasher.hashFile 摘要口径', () {
    test('空文件等于 BLAKE3 标准向量（空输入）', () async {
      final file = writeFile('empty.bin', const <int>[]);
      final hex = await FileHasher.hashFile(file.path);
      expect(
        hex,
        'af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262',
      );
    });

    test('单块文件与内存实现逐字节一致', () async {
      final bytes = sampleBytes(1000);
      final file = writeFile('small.bin', bytes);
      expect(await FileHasher.hashFile(file.path), FileHasher.hashBytes(bytes));
    });

    test('跨多个读块的文件与内存实现逐字节一致', () async {
      final bytes = sampleBytes(300 * 1024);
      final file = writeFile('multi_block.bin', bytes);
      final hex = await FileHasher.hashFile(
        file.path,
        blockSize: 64 * 1024, // 4 块 + 尾巴，覆盖 _addChunk 的 cv stack 合并
      );
      expect(hex, FileHasher.hashBytes(bytes));
      expect(hex.length, 64);
    });

    test('prefix 按「prefix 字节 + 文件字节」入哈希', () async {
      final bytes = sampleBytes(4096);
      final file = writeFile('prefixed.bin', bytes);
      const prefix = 'v3\n设备A\n';
      final hex = await FileHasher.hashFile(file.path, prefix: prefix);
      final expected = FileHasher.hashBytes(<int>[
        ...utf8.encode(prefix),
        ...bytes,
      ]);
      expect(hex, expected);

      // 反向校验：同样的文件换 prefix 必须换摘要，否则备份去重会误判为未变更
      final other = await FileHasher.hashFile(file.path, prefix: 'v3\n设备B\n');
      expect(other, isNot(hex));
    });
  });

  group('FileHasher.hashFile 进度', () {
    test('先回调 (0,total)，末次 (total,total)，且单调递增', () async {
      const total = 200 * 1024;
      final file = writeFile('progress.bin', sampleBytes(total));

      final events = <({int hashed, int total})>[];
      await FileHasher.hashFile(
        file.path,
        blockSize: 64 * 1024,
        onProgress: (hashed, size) => events.add((hashed: hashed, total: size)),
      );

      expect(events, isNotEmpty);
      expect(events.first, (hashed: 0, total: total));
      expect(events.last, (hashed: total, total: total));
      for (var i = 1; i < events.length; i++) {
        expect(events[i].hashed, greaterThan(events[i - 1].hashed));
        expect(events[i].total, total);
      }
      // 200 KiB / 64 KiB → 3 整块 + 1 尾块，共 4 次读取（含初始 0 共 5 条）
      expect(events.length, 5);
    });

    test('不传 onProgress 也能正常算完（后台备份走这条路径）', () async {
      final bytes = sampleBytes(1024);
      final file = writeFile('no_progress.bin', bytes);
      expect(await FileHasher.hashFile(file.path), FileHasher.hashBytes(bytes));
    });
  });

  group('FileHasher.hashFile 错误处理', () {
    test('文件不存在抛 FileHashException（不悬挂、不返回空摘要）', () async {
      await expectLater(
        FileHasher.hashFile('${tmp.path}/does_not_exist.bin'),
        throwsA(isA<FileHashException>()),
      );
    });

    test('blockSize <= 0 直接抛错，避免死循环', () async {
      final file = writeFile('block.bin', sampleBytes(16));
      await expectLater(
        FileHasher.hashFile(file.path, blockSize: 0),
        throwsA(isA<FileHashException>()),
      );
    });
  });

  // 与后端实现交叉对拍的固定向量：由 Go 侧 `lukechampine.com/blake3`（后端
  // `internal/service/upload.go`、`internal/download` 用的同一个库）对同样的
  // 字节序列算出。任何一侧换了实现/口径都会在这里红灯，避免「客户端和服务端
  // 各算各的摘要」这种最坏的静默失败（上传完成时会报 file hash mismatch）。
  group('与后端 lukechampine.com/blake3 交叉对拍', () {
    test('300 KiB 确定性模式', () async {
      final file = writeFile('xcheck.bin', sampleBytes(300 * 1024));
      expect(
        await FileHasher.hashFile(file.path, blockSize: 64 * 1024),
        'aa6ddbd833cca320e8a72567d12f0ae2e88e4661632dc0b1d55eaecb00998818',
      );
    });

    test('prefix + 内容（设备备份的 hash 规则）', () async {
      final file = writeFile('xcheck_prefix.bin', sampleBytes(300 * 1024));
      expect(
        await FileHasher.hashFile(
          file.path,
          prefix: 'v3\n设备A\n',
          blockSize: 64 * 1024,
        ),
        'c77c4a376157792b7b1e37ac98c6fda429a33c8a055021ff9ae568d2b9bbaeab',
      );
    });
  });

  group('FileHasher.hashBytes', () {
    test('输出 64 字符小写 hex，不截断（32 字节摘要）', () {
      final hex = FileHasher.hashBytes(utf8.encode('pfm'));
      expect(hex.length, 64);
      expect(hex, hex.toLowerCase());
      expect(RegExp(r'^[0-9a-f]{64}$').hasMatch(hex), isTrue);
    });
  });

  group('原生后端（该平台编了库才跑）', () {
    final native = Blake3Native.instance;
    final skipReason = native == null
        ? '当前平台没有原生 BLAKE3 库（未构建或平台未覆盖），仅跑纯 Dart 路径'
        : null;

    test('加载后自检通过（ABI 版本 + 空输入标准向量）', () {
      // instance 非空就说明 _bind 里的 ABI 校验和空向量自检都过了
      expect(native, isNotNull);
    }, skip: skipReason);

    test('分片喂入与纯 Dart 一次性摘要逐字节一致', () {
      // 3 MiB + 零头：跨 BLAKE3 chunk（1024B）边界和更新内部的并行阈值（1 MiB）
      final data = sampleBytes(3 * 1024 * 1024 + 7);
      final hasher = native!.newHasher();
      try {
        for (var start = 0; start < data.length; start += 300 * 1024) {
          final end = min(start + 300 * 1024, data.length);
          hasher.updateBytes(Uint8List.sublistView(data, start, end));
        }
        expect(hasher.finalizeHex(), FileHasher.hashBytes(data));
      } finally {
        hasher.dispose();
      }
    }, skip: skipReason);

    test('dispose 幂等，dispose 后调用抛 StateError', () {
      final hasher = native!.newHasher();
      hasher.updateBytes(const [1, 2, 3]);
      hasher.finalizeHex();
      hasher.dispose();
      hasher.dispose();
      expect(() => hasher.updateBytes(const [4]), throwsStateError);
      expect(hasher.finalize, throwsStateError);
    }, skip: skipReason);

    test('hashFile 走原生路径时与 hashBytes 一致（含 prefix）', () async {
      final dir = await Directory.systemTemp.createTemp('pfm_native_hash');
      addTearDown(() => dir.delete(recursive: true));
      final data = sampleBytes(1024 * 1024 + 3);
      final file = File('${dir.path}/native.bin');
      await file.writeAsBytes(data);

      expect(
        await FileHasher.hashFile(file.path, blockSize: 256 * 1024),
        FileHasher.hashBytes(data),
      );
      final hasher = native!.newHasher();
      try {
        hasher.updateBytes(utf8.encode('v3\n设备A\n'));
        hasher.updateBytes(data);
        expect(
          await FileHasher.hashFile(
            file.path,
            prefix: 'v3\n设备A\n',
            blockSize: 256 * 1024,
          ),
          hasher.finalizeHex(),
        );
      } finally {
        hasher.dispose();
      }
    }, skip: skipReason);
  });
}
