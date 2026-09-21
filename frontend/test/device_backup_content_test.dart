import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/services/device_backup_content.dart';

/// 短信/通话记录备份的"标明来源设备"逻辑单测。
///
/// 背景：短信与通话记录**按设备独有**（不像通讯录那样跨设备同步），
/// 多设备备份到同一目录后会混作一谈，因此文件名与文件内容都必须带设备标识。
void main() {
  group('sanitizeDeviceLabel', () {
    test('普通设备名原样保留', () {
      expect(sanitizeDeviceLabel('Xiaomi-23127PN0CC'), 'Xiaomi-23127PN0CC');
      expect(sanitizeDeviceLabel('Redmi K40'), 'Redmi_K40');
    });

    test('路径与 CSV 危险字符替换为下划线', () {
      // 斜杠会破坏路径、逗号/引号会破坏 CSV 列、换行会破坏 CSV 行
      expect(sanitizeDeviceLabel('a/b\\c'), 'a_b_c');
      expect(sanitizeDeviceLabel('a,b"c'), 'a_b_c');
      expect(sanitizeDeviceLabel('line1\nline2'), 'line1_line2');
      expect(sanitizeDeviceLabel('tab\there'), 'tab_here');
    });

    test('连续下划线折叠、首尾下划线/点去掉', () {
      expect(sanitizeDeviceLabel('a  //  b'), 'a_b');
      expect(sanitizeDeviceLabel('...device...'), 'device');
      expect(sanitizeDeviceLabel('_leading'), 'leading');
    });

    test('超长名称截断到上限（防止撑爆文件名）', () {
      final long = 'x' * 100;
      expect(sanitizeDeviceLabel(long).length, kDeviceLabelMaxChars);
    });

    test('空/全非法字符退回兜底值（文件名不能没有设备段）', () {
      expect(sanitizeDeviceLabel(''), kDeviceLabelFallback);
      expect(sanitizeDeviceLabel('   '), kDeviceLabelFallback);
      expect(sanitizeDeviceLabel('///'), kDeviceLabelFallback);
    });
  });

  group('buildDeviceInfo（设备类型_标识）', () {
    test('类型与标识都有 → 下划线拼接', () {
      expect(
        buildDeviceInfo(deviceType: '华为畅享 60X', deviceId: 'huaweufhfg'),
        '华为畅享_60X_huaweufhfg',
      );
    });

    test('空格被替换、标识原样保留', () {
      expect(
        buildDeviceInfo(deviceType: 'Xiaomi 14', deviceId: 'my-phone'),
        'Xiaomi_14_my-phone',
      );
    });

    test('只有类型时不补占位符', () {
      expect(buildDeviceInfo(deviceType: '华为畅享60X', deviceId: ''), '华为畅享60X');
    });

    test('只有标识时只输出标识', () {
      expect(buildDeviceInfo(deviceType: '', deviceId: 'huaweufhfg'), 'huaweufhfg');
    });

    test('两段都空 → 兜底值（文件名不能没有设备段）', () {
      expect(buildDeviceInfo(deviceType: '', deviceId: ''), kDeviceLabelFallback);
      expect(buildDeviceInfo(deviceType: '  ', deviceId: '///'), kDeviceLabelFallback);
    });

    test('类型里混入的旧兜底字面量不算有效段', () {
      // sanitizeDeviceLabel 会把无效输入变成 unknown_device，
      // 该段应被视为"没有值"，而不是真叫 unknown_device 的设备
      expect(
        buildDeviceInfo(deviceType: kDeviceLabelFallback, deviceId: 'x'),
        'x',
      );
    });
  });

  group('buildDeviceBackupFileName', () {
    test('设备信息在时间戳之前，且文件名不含路径分隔符', () {
      final name = buildDeviceBackupFileName(
        prefix: 'sms',
        deviceInfo: 'Xiaomi/14_my-phone',
        now: DateTime(2026, 9, 21, 8, 5),
        extension: 'xml',
      );
      expect(name, 'sms_backup_Xiaomi_14_my-phone_20260921_0805.xml');
      expect(name.contains('/'), isFalse);
      expect(name.contains('\\'), isFalse);
    });

    test('真实的华为设备名（类型+标识）', () {
      final name = buildDeviceBackupFileName(
        prefix: 'sms',
        deviceInfo: buildDeviceInfo(deviceType: '华为畅享 60X', deviceId: 'huaweufhfg'),
        now: DateTime(2026, 9, 21, 8, 5),
        extension: 'xml',
      );
      // "华为畅享 60X" 里的空格被 sanitize 成下划线
      expect(name, 'sms_backup_华为畅享_60X_huaweufhfg_20260921_0805.xml');
    });

    test('通话记录用 call_log 前缀与 csv 后缀', () {
      final name = buildDeviceBackupFileName(
        prefix: 'call_log',
        deviceInfo: 'Redmi_K40',
        now: DateTime(2026, 1, 2, 3, 4),
        extension: 'csv',
      );
      expect(name, 'call_log_backup_Redmi_K40_20260102_0304.csv');
    });

    test('同型号两台设备生成的备份文件名不同（核心诉求：可区分来源）', () {
      final now = DateTime(2026, 9, 21, 8, 5);
      // 同为 "华为畅享 60X"，仅标识不同
      final a = buildDeviceBackupFileName(
        prefix: 'sms',
        deviceInfo: buildDeviceInfo(deviceType: '华为畅享 60X', deviceId: 'phone-a'),
        now: now,
        extension: 'xml',
      );
      final b = buildDeviceBackupFileName(
        prefix: 'sms',
        deviceInfo: buildDeviceInfo(deviceType: '华为畅享 60X', deviceId: 'phone-b'),
        now: now,
        extension: 'xml',
      );
      expect(a, isNot(b));
      expect(a, contains('华为畅享_60X'));
      expect(a, contains('phone-a'));
    });
  });

  group('buildDeviceBackupContent - 短信 XML', () {
    final records = [
      '{"id":1,"address":"+8613800000000","date":1700000000000,"body":"hi <b> & \\"q\\"","type":1,"read":1}',
      '{"id":2,"address":"10086","date":1700000001000,"body":"balance","type":2,"read":0}',
    ];

    test('带设备标识注释，且仍兼容 SMS Backup & Restore 的 <smses> 结构', () {
      final xml = buildDeviceBackupContent(
        isSms: true,
        deviceInfo: 'Xiaomi_14_my-phone',
        rawJsonRecords: records,
      );
      expect(xml, contains('<?xml version="1.0" encoding="UTF-8"?>'));
      expect(
        xml,
        contains(
          '<!-- backup_device="Xiaomi_14_my-phone" -->',
        ),
      );
      // count 必须是真实记录数（恢复端依赖它校验）
      expect(xml, contains('<smses count="2">'));
      expect(xml, contains('</smses>'));
      // 记录体保留原字段
      expect(xml, contains('address="+8613800000000"'));
      expect(xml, contains('date="1700000000000"'));
    });

    test('短信正文里的 XML 特殊字符被转义', () {
      final xml = buildDeviceBackupContent(
        isSms: true,
        deviceInfo: 'dev_m',
        rawJsonRecords: records,
      );
      expect(xml, contains('&lt;b&gt;'));
      expect(xml, contains('&amp;'));
      expect(xml, contains('&quot;q&quot;'));
      // 未转义的原文字符串不应出现，否则 XML 非法
      expect(xml.contains('body="hi <b>'), isFalse);
    });

    test('设备名里的引号/尖括号不会破坏注释或 XML', () {
      // 走完整的 buildDeviceInfo 链路：类型含 &（XML 特殊字符）与空格，
      // 标识含引号（sanitize 阶段被替换成下划线）
      final info = buildDeviceInfo(deviceType: 'M&M Phone', deviceId: 'a"b');
      final xml = buildDeviceBackupContent(
        isSms: true,
        deviceInfo: info,
        rawJsonRecords: const [],
      );
      // & 不在 sanitize 的替换集里（它是 XML 转义的事，不是文件名合法性的事），
      // 故类型段保留 & —— 只要最终 XML 里被转义成 &amp; 即合法；
      // 引号则被 sanitize 成下划线（"a\"b" -> "a_b"）
      expect(xml, contains('backup_device="M&amp;M_Phone_a_b"'));
      expect(xml, contains('<smses count="0">'));
    });

    test('分块拼接结果与一次性生成逐字节相同', () {
      final oneShot = buildDeviceBackupContent(
        isSms: true,
        deviceInfo: 'Xiaomi_14_my-phone',
        rawJsonRecords: records,
      );

      final chunked = StringBuffer()
        ..write(
          buildDeviceBackupContent(
            isSms: true,
            deviceInfo: 'Xiaomi_14_my-phone',
            rawJsonRecords: records.sublist(0, 1),
            includeHeader: true,
            isLastChunk: false,
            totalCount: records.length,
          ),
        )
        ..write(
          buildDeviceBackupContent(
            isSms: true,
            deviceInfo: 'Xiaomi_14_my-phone',
            rawJsonRecords: records.sublist(1),
            includeHeader: false,
            isLastChunk: true,
            totalCount: records.length,
          ),
        );

      expect(chunked.toString(), oneShot);
    });
  });

  group('buildDeviceBackupContent - 通话记录 CSV', () {
    final records = [
      '{"id":1,"number":"10086","date":1700000000000,"duration":12,"type":1,"name":"客服","isNew":0}',
      '{"id":2,"number":"+8613900000000","date":1700000060000,"duration":0,"type":3,"name":"","isNew":1}',
    ];

    test('首行标明设备，表头与数据行齐全', () {
      final csv = buildDeviceBackupContent(
        isSms: false,
        deviceInfo: 'Xiaomi_14_my-phone',
        rawJsonRecords: records,
      );
      final lines = csv.trim().split('\n');
      expect(
        lines.first,
        '# backup_device="Xiaomi_14_my-phone"',
      );
      expect(lines[1], '时间,号码,姓名,类型,时长(秒)');
      expect(lines.length, 4);
      expect(lines[2], contains('10086'));
      expect(lines[2], contains('来电'));
      expect(lines[3], contains('未接'));
    });

    test('含逗号/引号的姓名被正确引号包裹（不破坏列数）', () {
      final csv = buildDeviceBackupContent(
        isSms: false,
        deviceInfo: 'dev_m',
        rawJsonRecords: const [
          '{"number":"100","date":1700000000000,"duration":1,"type":2,"name":"李,四","isNew":0}',
        ],
      );
      expect(csv, contains('"李,四"'));
    });

    test('后续分块不重复输出表头', () {
      final tail = buildDeviceBackupContent(
        isSms: false,
        deviceInfo: 'dev_m',
        rawJsonRecords: records,
        includeHeader: false,
      );
      expect(tail.contains('backup_device'), isFalse);
      expect(tail.contains('时间,号码'), isFalse);
      expect(tail, contains('10086'));
    });
  });
}
