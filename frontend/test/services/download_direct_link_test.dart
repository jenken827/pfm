import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/download.dart';

/// 直链下载（客户端阶段 2）的纯逻辑单测。
///
/// 这里只验"什么时候允许用直链、什么状态码算探测通过"这两条判定规则：
/// 真实取链与探测都要连云盘，不在单测里做（也不 mock 网络）。
void main() {
  group('isDirectLinkProbeUsable', () {
    test('206 才是可用（提供者真的认 Range，多线程与续传才成立）', () {
      expect(isDirectLinkProbeUsable(206), isTrue);
    });

    test('200 不可用（提供者忽略 Range，直链只能整文件重下）', () {
      expect(isDirectLinkProbeUsable(200), isFalse);
    });

    test('403/404/410 等一律不可用（签名过期或 IP 不符）', () {
      for (final statusCode in [403, 404, 410, 416, 500, 502, 0]) {
        expect(
          isDirectLinkProbeUsable(statusCode),
          isFalse,
          reason: 'HTTP $statusCode',
        );
      }
    });
  });

  group('shouldTryDirectLink', () {
    test('没有 repository 时不走直链（行为与改造前完全一致）', () {
      expect(
        shouldTryDirectLink(fileId: 'abc123', hasRepository: false),
        isFalse,
      );
    });

    test('历史版本下载不走直链（没有存储提供者位置信息）', () {
      expect(
        shouldTryDirectLink(fileId: 'history:42', hasRepository: true),
        isFalse,
      );
    });

    test('普通文件且拿到 repository 时才尝试直链', () {
      expect(
        shouldTryDirectLink(fileId: 'abc123', hasRepository: true),
        isTrue,
      );
    });
  });

  group('DirectLinkProbeResult', () {
    test('可用结论带状态码，reason 只含状态码不含 URL', () {
      const ok = DirectLinkProbeResult(
        usable: true,
        statusCode: 206,
        reason: 'HTTP 206',
      );
      expect(ok.usable, isTrue);
      expect(ok.statusCode, 206);
      expect(ok.reason, 'HTTP 206');

      const bad = DirectLinkProbeResult(
        usable: false,
        reason: 'probe TimeoutException',
      );
      expect(bad.usable, isFalse);
      expect(bad.statusCode, isNull);
      expect(bad.reason, isNot(contains('http')));
    });
  });

  group('redactDirectLink', () {
    // package:http 的 ClientException.toString() 会拼出 uri=<请求地址>，
    // 直链模式下这句话会随"下载失败"进日志与下载记录的 errorMessage（还会上屏）。
    const link = 'https://provider.example.com/file?sign=SECRET';

    test('直链模式下把整条凭证从异常文本里抹掉', () {
      final text = redactDirectLink(
        'ClientException: Connection closed, uri=$link',
        link,
      );
      expect(text.contains(link), isFalse);
      expect(text.contains('SECRET'), isFalse);
      expect(text.contains('Connection closed'), isTrue);
    });

    test('代理模式下（没有直链）原样返回，行为与改造前一致', () {
      const text = 'ClientException: Connection closed, uri=/api/file/stream';
      expect(redactDirectLink(text, null), text);
      expect(redactDirectLink(text, ''), text);
    });
  });
}
