import 'package:flutter_test/flutter_test.dart';
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
}
