import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/video_transcode.dart';

void main() {
  group('TranscodeProbeResult.fromResponse', () {
    test('200 / 206 视为就绪，进度 100', () {
      for (final code in [200, 206]) {
        final result = TranscodeProbeResult.fromResponse(code, null);
        expect(result.state, TranscodeState.ready, reason: 'HTTP $code');
        expect(result.progress, 100);
        expect(result.statusCode, code);
      }
    });

    test('202 解析转码中 / 排队中与进度', () {
      final running = TranscodeProbeResult.fromResponse(202, {
        'state': 'running',
        'progress': 42,
      });
      expect(running.state, TranscodeState.running);
      expect(running.progress, 42);

      final queued = TranscodeProbeResult.fromResponse(202, {
        'state': 'queued',
        'progress': 0,
      });
      expect(queued.state, TranscodeState.queued);
      expect(queued.shouldContinue, isTrue);
    });

    test('202 缺字段 / 越界进度不炸', () {
      final missing = TranscodeProbeResult.fromResponse(202, null);
      expect(missing.state, TranscodeState.running);
      expect(missing.progress, 0);

      final weird = TranscodeProbeResult.fromResponse(202, {
        'progress': 140,
      });
      expect(weird.progress, 100, reason: '进度夹到 0~100');
    });

    test('501 是服务端没装 ffmpeg，403 是权限问题', () {
      expect(
        TranscodeProbeResult.fromResponse(501, null).state,
        TranscodeState.disabled,
      );
      expect(
        TranscodeProbeResult.fromResponse(403, null).state,
        TranscodeState.failed,
      );
      expect(
        TranscodeProbeResult.fromResponse(0, null).state,
        TranscodeState.networkError,
      );
    });
  });

  group('pollTranscode', () {
    test('转码中继续轮询，就绪后返回并上报进度', () async {
      final results = [
        TranscodeProbeResult.fromResponse(202, {'state': 'running', 'progress': 10}),
        TranscodeProbeResult.fromResponse(202, {'state': 'running', 'progress': 60}),
        TranscodeProbeResult.fromResponse(206, null),
      ];
      var index = 0;
      final updates = <int>[];

      final result = await pollTranscode(
        probe: () async => results[index++],
        interval: Duration.zero,
        onUpdate: (update) => updates.add(update.progress),
      );

      expect(result.state, TranscodeState.ready);
      expect(index, 3, reason: '就绪后不再探测');
      expect(updates, [10, 60, 100]);
    });

    test('网络抖动会重试，连续异常过多才放弃', () async {
      final results = [
        TranscodeProbeResult.fromResponse(0, null),
        TranscodeProbeResult.fromResponse(0, null),
        TranscodeProbeResult.fromResponse(206, null),
      ];
      var index = 0;
      final result = await pollTranscode(
        probe: () async => results[index++],
        interval: Duration.zero,
        onUpdate: (_) => index = index,
      );
      expect(result.state, TranscodeState.ready);

      var errors = 0;
      final failed = await pollTranscode(
        probe: () async {
          errors++;
          return TranscodeProbeResult.fromResponse(0, null);
        },
        interval: Duration.zero,
      );
      expect(failed.state, TranscodeState.networkError);
      expect(errors, 3, reason: '连续 3 次网络异常就放弃，不会一直转圈');
    });

    test('501 / 403 立即返回，不做无意义的轮询', () async {
      var calls = 0;
      final result = await pollTranscode(
        probe: () async {
          calls++;
          return TranscodeProbeResult.fromResponse(501, null);
        },
        interval: Duration.zero,
      );
      expect(result.state, TranscodeState.disabled);
      expect(calls, 1);
    });

    test('取消与超时都有明确状态', () async {
      final cancelled = await pollTranscode(
        probe: () async => TranscodeProbeResult.fromResponse(202, null),
        interval: Duration.zero,
        isCancelled: () => true,
      );
      expect(cancelled.state, TranscodeState.cancelled);

      final timeout = await pollTranscode(
        probe: () async => TranscodeProbeResult.fromResponse(202, null),
        interval: Duration.zero,
        maxAttempts: 2,
      );
      expect(timeout.state, TranscodeState.timeout);
    });
  });
}
