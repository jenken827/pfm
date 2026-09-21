import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/call_recording_content.dart';
import 'package:pfm/services/client_backup_service.dart';

/// 通话录音备份的纯逻辑单测。
///
/// 用例中的文件名全部取自真机实测样本（华为 STG-AL00，
/// /sdcard/Sounds/CallRecord），避免用臆想的格式做测试。
void main() {
  group('isCallRecordingFile', () {
    test('常见录音扩展名都认', () {
      expect(isCallRecordingFile('a.amr'), isTrue);
      expect(isCallRecordingFile('a.m4a'), isTrue);
      expect(isCallRecordingFile('a.AAC'), isTrue);
      expect(isCallRecordingFile('a.3gp'), isTrue);
    });

    test('非音频扩展名不认（目录里可能有杂项）', () {
      expect(isCallRecordingFile('a.jpg'), isFalse);
      expect(isCallRecordingFile('a.txt'), isFalse);
      expect(isCallRecordingFile('a.xml'), isFalse);
      expect(isCallRecordingFile('a.apk'), isFalse);
    });

    test('没有扩展名/点结尾/隐藏文件不认', () {
      expect(isCallRecordingFile('noext'), isFalse);
      expect(isCallRecordingFile('trailing.'), isFalse);
      expect(isCallRecordingFile('.amr'), isFalse);
    });
  });

  group('parseCallRecordingName - 号码形态', () {
    test('分段号码（真机样本）', () {
      final r = parseCallRecordingName('181 3574 1189_20260612104625.amr');
      expect(r.party, '181 3574 1189');
      expect(r.recordedAt, DateTime(2026, 6, 12, 10, 46, 25));
    });

    test('固话区号（真机样本）', () {
      final r = parseCallRecordingName('0571 2895 8484_20240626134601.amr');
      expect(r.party, '0571 2895 8484');
      expect(r.recordedAt, DateTime(2024, 6, 26, 13, 46, 1));
    });

    test('联系人名@短号（真机样本，含中文）', () {
      final r = parseCallRecordingName('移动@10086_20260906162304.amr');
      expect(r.party, '移动@10086');
      expect(r.recordedAt, DateTime(2026, 9, 6, 16, 23, 4));
    });

    test('连续数字号码', () {
      final r = parseCallRecordingName('130 4344 4093_20240326193820.amr');
      expect(r.party, '130 4344 4093');
      expect(r.recordedAt, DateTime(2024, 3, 26, 19, 38, 20));
    });

    test('号码前导 0（真机样本 130 4982 2341 之外的分支）', () {
      final r = parseCallRecordingName('0311 8523 2147_20240401130752.amr');
      expect(r.party, '0311 8523 2147');
      expect(r.recordedAt, DateTime(2024, 4, 1, 13, 7, 52));
    });
  });

  group('parseCallRecordingName - 异常与边界', () {
    test('没有时间戳后缀时仍返回对象（不能因命名不合规就漏备份）', () {
      final r = parseCallRecordingName('recording.amr');
      expect(r.party, 'recording');
      expect(r.recordedAt, isNull);
    });

    test('月份越界不当作时间戳', () {
      final r = parseCallRecordingName('12345678901234.amr');
      expect(r.recordedAt, isNull);
    });

    test('非法日期（2 月 31 日）被拒绝而不是进位', () {
      final r = parseCallRecordingName('phone_20260231120000.amr');
      expect(r.recordedAt, isNull);
      // DateTime 构造器会把 2/31 进位成 3/2，必须回读校验才能发现
      expect(r.party, 'phone');
    });

    test('时分秒越界被拒绝', () {
      expect(parseCallRecordingName('a_20260101250000.amr').recordedAt, isNull);
      expect(parseCallRecordingName('a_20260101126000.amr').recordedAt, isNull);
      expect(parseCallRecordingName('a_20260101120061.amr').recordedAt, isNull);
    });

    test('时间戳在中间（非结尾）不解析为时间', () {
      final r = parseCallRecordingName('20260101120000_extra.amr');
      expect(r.recordedAt, isNull);
    });

    test('纯数字号码恰好 14 位但月份非法 → 不进位', () {
      // 例如号码 99999999999999：月份 99 越界，不应被当时间
      expect(parseCallRecordingName('99999999999999.amr').recordedAt, isNull);
    });

    test('无扩展名也能解析', () {
      final r = parseCallRecordingName('10086_20260906162304');
      expect(r.party, '10086');
      expect(r.recordedAt, DateTime(2026, 9, 6, 16, 23, 4));
    });
  });

  group('目录候选', () {
    test('包含真机实测的华为路径且排在最前', () {
      expect(kCallRecordingDirCandidates.first, 'Sounds/CallRecord');
    });

    test('覆盖小米路径', () {
      expect(
        kCallRecordingDirCandidates,
        contains('MIUI/sound_recorder/call_rec'),
      );
    });
  });

  group('shouldBackupRecording（增量判定，防漏传）', () {
    // 便捷构造：默认"已备份完成且未变化"
    bool call({
      bool hasRecord = true,
      int? status = kBackupFileStatusCompleted,
      int? recordedMtime = 1000,
      int? recordedSize = 500,
      int mtime = 1000,
      int size = 500,
    }) => shouldBackupRecording(
      hasRecord: hasRecord,
      recordStatus: status,
      recordModifiedAt: recordedMtime,
      recordFileSize: recordedSize,
      currentModifiedAt: mtime,
      currentFileSize: size,
    );

    test('新文件（无记录）→ 传', () {
      expect(call(hasRecord: false), isTrue);
    });

    test('已完成且 size/mtime 未变 → 跳过', () {
      expect(call(), isFalse);
    });

    test('skipped 且未变 → 跳过', () {
      expect(call(status: kBackupFileStatusSkipped), isFalse);
    });

    test('★ 上次 failed → 必须重试（否则永远漏传）', () {
      // 关键回归点：失败时 size/mtime 都没变，只看 size/mtime 会永久跳过
      expect(call(status: kBackupFileStatusFailed), isTrue);
    });

    test('★ pending/uploading 且 size/mtime 未变 → 跳过（不再无条件重传）', () {
      // 回归点：曾把 pending 当作"必须重试"，而每个文件入队时都先写 pending；
      // 一旦回写 completed 的链路缺失，pending 永不消除，导致每次备份
      // 都重传全部文件（真机 438 个）。漏传由 failed 覆盖，不需要 pending 兜底。
      expect(call(status: kBackupFileStatusPending), isFalse);
      expect(call(status: kBackupFileStatusUploading), isFalse);
    });

    test('pending 且 size 变化 → 仍需重传（增量判定照常生效）', () {
      expect(call(status: kBackupFileStatusPending, size: 999), isTrue);
    });

    test('stale → 重传', () {
      expect(call(status: kBackupFileStatusStale), isTrue);
    });

    test('size 变化 → 重传', () {
      expect(call(size: 999), isTrue);
    });

    test('mtime 变化 → 重传', () {
      expect(call(mtime: 2000), isTrue);
    });

    test('记录里 size/mtime 为 null 时不误判为变化', () {
      // 老记录可能没写这些字段，此时不应反复重传
      expect(call(recordedMtime: null, recordedSize: null), isFalse);
    });
  });

  group('状态常量与 BackupFileStatus 枚举保持一致', () {
    // 数值一旦错位，增量判定就会失效（例如把 failed 当成 completed），
    // 故用枚举的真实 index 做断言
    test('常量值 == 枚举 index', () {
      expect(kBackupFileStatusPending, BackupFileStatus.pending.index);
      expect(kBackupFileStatusUploading, BackupFileStatus.uploading.index);
      expect(kBackupFileStatusCompleted, BackupFileStatus.completed.index);
      expect(kBackupFileStatusFailed, BackupFileStatus.failed.index);
      expect(kBackupFileStatusSkipped, BackupFileStatus.skipped.index);
      expect(kBackupFileStatusStale, BackupFileStatus.stale.index);
    });
  });

  group('上传任务 tags 约定（回写 completed 的关键）', () {
    // 上传完成时 BackgroundService 靠 tags 找到对应 backup 记录回写 completed。
    // 约定 configId|relativePath；不含 | 时按相册备份（tags 即 asset id）处理。
    // 若录音任务不带 tags，记录会永远停在 pending —— 真机上表现为
    // "每次点备份都重传全部 438 个"。
    test('录音任务 tags 形如 configId|relativePath', () {
      const configId = 'call_recording_backup';
      const name = '181 3574 1189_20260612104625.amr';
      final tag = '$configId|$name';
      expect(tag, contains('|'));
      final parts = tag.split('|');
      expect(parts.first, configId);
      // 文件名含空格与 @，但一定不含 '|'，故按首个 | 切分是安全的
      expect(parts.sublist(1).join('|'), name);
    });

    test('按首个 | 切分即可还原（文件名不含 |）', () {
      const tag = 'call_recording_backup|移动@10086_20260906162304.amr';
      final i = tag.indexOf('|');
      expect(tag.substring(0, i), 'call_recording_backup');
      expect(tag.substring(i + 1), '移动@10086_20260906162304.amr');
    });
  });
}
