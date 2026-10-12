import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/utils/single_instance.dart';

void main() {
  group('currentExeName', () {
    test('返回不含目录的小写 exe 名', () {
      final name = currentExeName;
      expect(name, isNotEmpty);
      expect(name, name.toLowerCase());
      expect(name.contains('/'), isFalse);
      expect(name.contains(r'\'), isFalse);
    });
  });

  group('findProcessesNamed', () {
    test('进程表中必定能找到本进程', () {
      final found = findProcessesNamed(currentExeName);
      expect(
        found,
        isNotEmpty,
        reason: '本进程自身必须在进程表里能被查到，'
            '否则单实例检查会把首次启动误判为重复实例',
      );
    });

    test('不存在的 exe 名返回空列表（fail-open）', () {
      expect(findProcessesNamed('no_such_process_xyz_123.exe'), isEmpty);
    });
  });

  // 用**构造的**进程表钉住匹配规则：真实进程表随环境变化，
  // 而这里要防的是规则性缺陷（见 BUGS.md「单实例检查误判已有实例在跑」）。
  group('parsePsOutput 匹配规则', () {
    test('只挑同名进程', () {
      const ps = '  1234 pfm\n  5678 other\n';
      expect(parsePsOutput(ps, 'pfm'), [1234]);
    });

    test('comm 结尾带 / 的内核线程不算同名进程', () {
      // 核心回归点：`nvidia-modeset/` 经 split('/').last 得到**空串**，
      // 而 `target.startsWith('')` 恒为真 → 会被误当成同名进程，
      // 导致 hasOtherInstanceRunning() 无端返回 true、应用拒绝启动。
      const ps = '  285 nvidia-modeset/\n  286 nvidia-modeset/\n  1234 pfm\n';
      expect(parsePsOutput(ps, 'pfm'), [1234]);
    });

    test('短 comm 不会前缀命中更长的目标名', () {
      const ps = '  1 1\n  2 1panel\n';
      expect(parsePsOutput(ps, '1panel'), [2]);
    });

    test('被截断到 15 字符的 comm 仍按前缀命中', () {
      const ps = '  7 dart:flutter_to\n';
      expect(parsePsOutput(ps, 'dart:flutter_tool'), [7]);
    });

    test('忽略空行与非数字 PID', () {
      const ps = '\n  \n  abc pfm\n  42 pfm\n';
      expect(parsePsOutput(ps, 'pfm'), [42]);
    });
  });

  group('hasOtherInstanceRunning', () {
    test('只有本进程时返回 false', () {
      // 注入假进程表：`flutter test` 并发执行测试文件时进程表里会有多个
      // flutter_tester，真实进程表断言不了「只有自己」。
      // 核心回归点：旧实现会把 PID 复用的系统进程当成实例，导致反复启动失败。
      expect(hasOtherInstanceRunning('pfm', (_) => [1234]), isFalse);
    });

    test('存在第二个同名进程时返回 true', () {
      expect(hasOtherInstanceRunning('pfm', (_) => [1234, 5678]), isTrue);
    });

    test('传入不存在的 exe 名时返回 false', () {
      expect(hasOtherInstanceRunning('nope_not_real_987.exe'), isFalse);
    });
  });
}
