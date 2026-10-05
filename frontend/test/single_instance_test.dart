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

  group('hasOtherInstanceRunning', () {
    test('只有本进程时返回 false', () {
      // 这是本次修复的核心回归点：旧实现会把 PID 复用的系统进程
      // （如 svchost.exe）当成实例，导致反复启动失败。
      expect(hasOtherInstanceRunning(), isFalse);
    });

    test('传入不存在的 exe 名时返回 false', () {
      expect(hasOtherInstanceRunning('nope_not_real_987.exe'), isFalse);
    });
  });
}