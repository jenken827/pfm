import 'dart:io' as io;

import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;
import 'package:pfm/proto/native/file.pb.dart';
import 'package:pfm/services/photo_date_resolver.dart';

void main() {
  group('PhotoDateResolver.parseFromFilename', () {
    test('毫秒时间戳命名：1784212534296.png → 2026/7/16 22:35:34', () {
      final dt = PhotoDateResolver.parseFromFilename('1784212534296.png');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 7);
      expect(dt.day, 16);
      expect(dt.hour, 22);
      expect(dt.minute, 35);
      expect(dt.second, 34);
    });

    test('相机命名：IMG_20260811_063006.jpg → 2026/8/11', () {
      final dt = PhotoDateResolver.parseFromFilename('IMG_20260811_063006.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 8);
      expect(dt.day, 11);
    });

    test('IMG直接跟日期：IMG20150908xxx.jpg → 2015/9/8', () {
      final dt = PhotoDateResolver.parseFromFilename('IMG20150908xxx.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2015);
      expect(dt.month, 9);
      expect(dt.day, 8);
    });

    test('全景命名：PANO_20170121_xxxx.jpg → 2017/1/21', () {
      final dt = PhotoDateResolver.parseFromFilename('PANO_20170121_xxxx.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2017);
      expect(dt.month, 1);
      expect(dt.day, 21);
    });

    test('视频命名：VID_20150218_xxx.jpg → 2015/2/18', () {
      final dt = PhotoDateResolver.parseFromFilename('VID_20150218_xxx.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2015);
      expect(dt.month, 2);
      expect(dt.day, 18);
    });

    test('虚线日期截图：Screenshot_2018-12-31-xxx.jpg → 2018/12/31', () {
      final dt = PhotoDateResolver.parseFromFilename(
        'Screenshot_2018-12-31-xxx.jpg',
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2018);
      expect(dt.month, 12);
      expect(dt.day, 31);
    });

    test('虚线日期带时间：Screenshot_2018-12-31-21-37-41_xxx.png → 2018/12/31', () {
      final dt = PhotoDateResolver.parseFromFilename(
        'Screenshot_2018-12-31-21-37-41_xxx.png',
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2018);
      expect(dt.month, 12);
      expect(dt.day, 31);
    });

    test('截图命名：Screenshot_20240109_213741_1_com.tencent.mm.jpg → 2024/1/9',
        () {
      final dt = PhotoDateResolver.parseFromFilename(
        'Screenshot_20240109_213741_1_com.tencent.mm.jpg',
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2024);
      expect(dt.month, 1);
      expect(dt.day, 9);
    });

    test('微信转发：mmexport1713701399029.jpg → 2024/4/21 20:09:59', () {
      final dt = PhotoDateResolver.parseFromFilename('mmexport1713701399029.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2024);
      expect(dt.month, 4);
      expect(dt.day, 21);
      expect(dt.hour, 20);
      expect(dt.minute, 9);
    });

    test('microMsg前缀：microMsg.1713701399029.jpg → 2024/4/21 20:09:59', () {
      final dt = PhotoDateResolver.parseFromFilename(
        'microMsg.1713701399029.jpg',
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2024);
      expect(dt.month, 4);
      expect(dt.day, 21);
    });

    test('视频扩展名同样解析：mmexport1713701399029.mp4 → 2024/4/21', () {
      final dt = PhotoDateResolver.parseFromFilename(
        'mmexport1713701399029.mp4',
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2024);
      expect(dt.month, 4);
      expect(dt.day, 21);
    });

    test('微信拍摄：wx_camera_1725958479520.jpg → 2024/9/10 16:54:39', () {
      final dt = PhotoDateResolver.parseFromFilename('wx_camera_1725958479520.jpg');
      expect(dt, isNotNull);
      expect(dt!.year, 2024);
      expect(dt.month, 9);
      expect(dt.day, 10);
      expect(dt.hour, 16);
      expect(dt.minute, 54);
    });

    test('前两位为 12 的时间戳拒绝（可能是其他编号）', () {
      expect(PhotoDateResolver.parseFromFilename('12345678901.jpg'), isNull);
    });

    test('前两位大于当前时间戳前两位拒绝（未来时间戳不可能）', () {
      // 当前时间戳前两位（2026 年约为 17），构造 19/20 开头的时间戳
      expect(PhotoDateResolver.parseFromFilename('1955555555555.jpg'), isNull);
    });

    test('普通文件名返回 null', () {
      expect(PhotoDateResolver.parseFromFilename('document.pdf'), isNull);
      expect(PhotoDateResolver.parseFromFilename('my vacation photo.jpg'), isNull);
      expect(PhotoDateResolver.parseFromFilename(''), isNull);
    });

    test('小于10位数字不解析', () {
      expect(PhotoDateResolver.parseFromFilename('12345.jpg'), isNull);
    });
  });

  group('PhotoDateResolver.parseExifDateTime', () {
    test('标准 EXIF 格式 "2026:07:16 22:35:34"', () {
      final dt = PhotoDateResolver.parseExifDateTime('2026:07:16 22:35:34');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 7);
      expect(dt.day, 16);
      expect(dt.hour, 22);
      expect(dt.minute, 35);
      expect(dt.second, 34);
    });

    test('带连字符格式 "2026-07-16 22:35:34"', () {
      final dt = PhotoDateResolver.parseExifDateTime('2026-07-16 22:35:34');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 7);
      expect(dt.day, 16);
    });

    test('非法日期（2月30日）返回 null', () {
      expect(PhotoDateResolver.parseExifDateTime('2026:02:30 10:00:00'), isNull);
    });

    test('空/乱码返回 null', () {
      expect(PhotoDateResolver.parseExifDateTime(null), isNull);
      expect(PhotoDateResolver.parseExifDateTime(''), isNull);
      expect(PhotoDateResolver.parseExifDateTime('not a date'), isNull);
    });
  });

  group('PhotoDateResolver.parseCompactDate', () {
    test('合法 YYYYMMDD', () {
      final dt = PhotoDateResolver.parseCompactDate('20260811');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 8);
      expect(dt.day, 11);
    });

    test('非法月份/日期返回 null', () {
      expect(PhotoDateResolver.parseCompactDate('20261301'), isNull);
      expect(PhotoDateResolver.parseCompactDate('20260230'), isNull);
    });
  });

  group('PhotoDateResolver 自定义配置规则', () {
    test('配置规则整体替换默认表（只保留 XYZ_ 前缀）', () {
      final cfg = PhotoDateConfig(
        filenameRules: [
          PhotoDateFilenameRule(
            type: 'prefix_segment_date',
            prefixes: ['XYZ_'],
            segmentIndex: 1,
          ),
        ],
      );
      // 自定义规则命中
      expect(
        PhotoDateResolver.parseFromFilenameWithConfig(
          'XYZ_20260811_063006.jpg',
          cfg,
        )?.year,
        2026,
      );
      // 默认规则已被替换：不再命中
      expect(
        PhotoDateResolver.parseFromFilenameWithConfig(
          'IMG_20260811_063006.jpg',
          cfg,
        ),
        isNull,
      );
    });

    test('未知规则类型跳过，不崩溃（向前兼容）', () {
      final cfg = PhotoDateConfig(
        filenameRules: [
          PhotoDateFilenameRule(type: 'some_future_rule'),
          PhotoDateFilenameRule(
            type: 'prefix_segment_date',
            prefixes: ['IMG_'],
            segmentIndex: 1,
          ),
        ],
      );
      final dt = PhotoDateResolver.parseFromFilenameWithConfig(
        'IMG_20260811_063006.jpg',
        cfg,
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
    });
  });

  group('PhotoDateResolver.resolveFromFile 兜底链路', () {
    late io.Directory tmpDir;

    setUp(() {
      tmpDir = io.Directory.systemTemp.createTempSync('pfm_photo_date_');
    });

    tearDown(() {
      if (tmpDir.existsSync()) tmpDir.deleteSync(recursive: true);
    });

    io.File createFile(String name, {DateTime? mtime}) {
      final file = io.File(p.join(tmpDir.path, name))
        ..writeAsBytesSync([0xFF, 0xD8, 0xFF, 0xE0]);
      if (mtime != null) file.setLastModifiedSync(mtime);
      return file;
    }

    test('媒体库时间正常时直接使用，不读文件系统时间', () async {
      final file = createFile('doc_0001.jpg', mtime: DateTime(2010, 1, 1));
      final media = DateTime(2026, 3, 4, 5, 6, 7);
      final dt = await PhotoDateResolver.resolveFromFile(
        file.path,
        mediaDate: media,
      );
      expect(dt, media);
    });

    test('媒体库时间为 epoch 0 时回退到文件 mtime', () async {
      final file = createFile('doc_0002.jpg', mtime: DateTime(2019, 5, 6, 7, 8));
      final dt = await PhotoDateResolver.resolveFromFile(
        file.path,
        mediaDate: DateTime.fromMillisecondsSinceEpoch(0),
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2019);
      expect(dt.month, 5);
      expect(dt.day, 6);
    });

    test('文件名 / 媒体库都无时间且文件不存在时返回 null', () async {
      final dt = await PhotoDateResolver.resolveFromFile(
        p.join(tmpDir.path, 'missing.jpg'),
        mediaDate: DateTime.fromMillisecondsSinceEpoch(0),
      );
      expect(dt, isNull);
    });

    test('文件名命中时优先于 mtime 与媒体库时间', () async {
      final file = createFile(
        'Screenshot_2018-12-31-21-37-41_xxx.png',
        mtime: DateTime(2020, 2, 2),
      );
      final dt = await PhotoDateResolver.resolveFromFile(
        file.path,
        mediaDate: DateTime(2026, 1, 1),
      );
      expect(dt, isNotNull);
      expect(dt!.year, 2018);
      expect(dt.month, 12);
      expect(dt.day, 31);
    });

    test('老照片（2000 年前）不受年份下界限制', () async {
      final file = createFile(
        'IMG_19980812_101112.jpg',
        mtime: DateTime(1998, 8, 12),
      );
      final dt = await PhotoDateResolver.resolveFromFile(
        file.path,
        mediaDate: DateTime.fromMillisecondsSinceEpoch(0),
      );
      expect(dt, isNotNull);
      expect(dt!.year, 1998);
      expect(dt.month, 8);
      expect(dt.day, 12);
    });

    test('resolveFromFileSystemTime 读 mtime，缺失文件返回 null', () async {
      final mtime = DateTime(2021, 11, 12, 13, 14, 15);
      final file = createFile('doc_0003.jpg', mtime: mtime);
      final got = await PhotoDateResolver.resolveFromFileSystemTime(file.path);
      expect(got, mtime);
      final none = await PhotoDateResolver.resolveFromFileSystemTime(
        p.join(tmpDir.path, 'nope.jpg'),
      );
      expect(none, isNull);
    });
  });
}
