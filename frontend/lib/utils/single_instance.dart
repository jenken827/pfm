import 'dart:convert';
import 'dart:io';

/// 桌面端单实例检查：避免同一份数据被两个进程并发写坏。
///
/// ## 为什么不用「锁文件 + PID 存活」
///
/// 旧实现把 PID 写进锁文件，启动时用 `tasklist` 判断该 PID 是否存在。
/// 它有两个会直接导致「启动不了」的缺陷：
///
/// 1. **PID 复用**：进程被强杀（taskkill /F、OOM、崩溃）时清理钩子不执行，
///    锁文件残留旧 PID。Windows 随后会把该 PID 回收给无关进程
///    （最常见是 `svchost.exe`），下次启动查到「进程存在」于是判定自己
///    是重复实例并 `exit(0)`。表现就是反复删锁文件也起不来。
/// 2. **匹配本地化输出**：`tasklist` 找不到进程时输出的是本地化提示
///    （简中环境还会在 Dart 侧乱码成 GBK 乱码），按中文字符串判断
///    「进程不存在」在非中文 locale 下必然失效，反向误判。
///
/// ## 现判据：同名进程计数
///
/// 启动早期本进程自身已经在进程表里，且 exe 名与目标一致。因此
/// **进程表中同名进程 ≥ 2 个，即说明除自己之外还有实例在跑**。
/// 这个判据不依赖 PID、不依赖本地化文本，也不受 PID 复用影响：
/// 前一个实例死掉后计数自动回落到 1，新实例即可正常启动。
///
/// 探测失败时一律**放行**（fail-open）：宁可允许双开，也不要因为
/// 环境缺 `tasklist`/`ps` 而把用户彻底锁在门外。

/// 当前进程的可执行文件名（小写，不含目录）。
String get currentExeName =>
    Platform.resolvedExecutable.split(RegExp(r'[/\\]')).last.toLowerCase();

/// 列出进程表中与 [exeName] 同名的进程（exe 名小写比较）。
///
/// 任何探测异常都返回空列表，即视为「没有其他实例」。
List<int> findProcessesNamed(String exeName) {
  try {
    if (Platform.isWindows) {
      final result = Process.runSync('tasklist', ['/FO', 'CSV', '/NH']);
      if (result.exitCode != 0) return const [];
      final pids = <int>[];
      for (final line in const LineSplitter().convert(result.stdout.toString())) {
        final parsed = _parseTasklistCsvLine(line);
        if (parsed == null) continue;
        final (name, pid) = parsed;
        if (name == exeName.toLowerCase()) pids.add(pid);
      }
      return pids;
    }
    final result = Process.runSync('ps', ['-eo', 'pid=,comm=']);
    if (result.exitCode != 0) return const [];
    final pids = <int>[];
    final target = exeName.toLowerCase();
    for (final line in const LineSplitter().convert(result.stdout.toString())) {
      // 输出形如 "  1234 pfm"
      final trimmed = line.trim();
      if (trimmed.isEmpty) continue;
      final split = trimmed.indexOf(RegExp(r'\s'));
      if (split <= 0) continue;
      final pid = int.tryParse(trimmed.substring(0, split));
      if (pid == null) continue;
      final name = trimmed.substring(split + 1).trim();
      // comm 可能被截断到 15 字符，故取 basename 后再比。
      final base = name.split('/').last.toLowerCase();
      if (base == target || target.startsWith(base)) pids.add(pid);
    }
    return pids;
  } catch (_) {
    // tasklist / ps 不可用 → 放行启动。
    return const [];
  }
}

/// 解析 `tasklist /FO CSV /NH` 的一行，返回 (镜像名, PID)。
///
/// 只接受结构合法的进程行：至少 4 个字段、首字段为带引号且以 `.exe`
/// 结尾的镜像名、第二字段为纯数字 PID。找不到任务时 tasklist 输出的是
/// 一整句本地化提示（无引号、列数不足），必然被此处拒绝——因此本函数
/// 与操作系统语言、代码页都无关。
(String, int)? _parseTasklistCsvLine(String line) {
  final trimmed = line.trim();
  if (!trimmed.startsWith('"')) return null;

  final fields = <String>[];
  var i = 0;
  while (i < trimmed.length) {
    final quote = trimmed.indexOf('"', i);
    if (quote < 0) break;
    final next = trimmed.indexOf('"', quote + 1);
    if (next < 0) break;
    fields.add(trimmed.substring(quote + 1, next));
    i = next + 2; // 跳过闭合引号与随后的分隔逗号
  }
  if (fields.length < 4) return null;

  final name = fields[0];
  if (!name.toLowerCase().endsWith('.exe')) return null;
  final pid = int.tryParse(fields[1]);
  if (pid == null) return null;
  return (name.toLowerCase(), pid);
}

/// 是否已有另一个实例在运行。
///
/// 返回 true 时调用方应记录日志并退出。计数里包含本进程自身，
/// 所以阈值是 2。
bool hasOtherInstanceRunning([String? exeName]) {
  final name = exeName ?? currentExeName;
  return findProcessesNamed(name).length >= 2;
}