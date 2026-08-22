import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:hive_ce_flutter/hive_ce_flutter.dart';
import 'package:media_kit/media_kit.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/hive/hive.dart';
import 'package:pfm/platform/platform.dart';
import 'package:talker_flutter/talker_flutter.dart';
import 'services/services.dart';
import 'package:pfm/services/recent_files_storage.dart';
import 'router/router.dart';
import 'app.dart';

Future<void> main() async {
  // 整个启动流程（含 ensureInitialized 与 runApp）运行在同一个受控 zone：
  // 1. 捕获未处理的异步/框架异常并持久化；
  // 2. 避免 "Zone mismatch"（bindings 初始化与 runApp 必须同一 zone）。
  await runZonedGuarded(
    () async {
      WidgetsFlutterBinding.ensureInitialized();

      // 日志最先初始化：确保启动阶段（Hive/MediaKit/后台服务）的异常与时序
      // 都能被持久化记录，用于排查冷启动卡闪屏问题。
      await initTalkerLogger();

      // 启动时序打点：每步打印相对耗时，便于定位卡顿阶段
      final sw = Stopwatch()..start();
      void tick(String stage) {
        getLogger().info('[startup] $stage (+${sw.elapsedMilliseconds}ms)');
      }

      tick('talker initialized');

      // 桌面平台单实例检查
      if (_isDesktop()) {
        final lockFile = File(
          '${Platform.environment['TEMP'] ?? '/tmp'}/pfm_running.lock',
        );
        if (lockFile.existsSync()) {
          // 检查进程是否还活着
          try {
            final pid = int.parse(lockFile.readAsStringSync().trim());
            // 尝试检查进程是否存在
            // Windows: tasklist 即使找不到进程也返回 0，需要检查输出内容
            // Linux/Mac: kill -0 找不到进程返回非 0
            final result = Process.runSync(
              Platform.isWindows ? 'tasklist' : 'kill',
              Platform.isWindows ? ['/FI', 'PID eq $pid'] : ['-0', '$pid'],
            );
            if (Platform.isWindows) {
              // Windows tasklist 返回码不可靠，需要检查输出中是否包含实际进程信息
              // 如果输出只包含"信息"提示行（没有找到任务），则进程不存在
              final stdout = result.stdout.toString();
              final stderr = result.stderr.toString();
              final hasProcess =
                  result.exitCode == 0 &&
                  !stdout.contains('没有运行的任务匹配指定标准') &&
                  !stdout.contains('No tasks are running which match') &&
                  !stdout.contains('No running task') &&
                  stderr.isEmpty;
              if (hasProcess) {
                getLogger().warning('PFM is already running (PID: $pid)');
                exit(0);
              }
            } else {
              if (result.exitCode == 0) {
                getLogger().warning('PFM is already running (PID: $pid)');
                exit(0);
              }
            }
          } catch (_) {
            // 解析失败，锁文件无效，继续启动
          }
        }
        // 写入当前 PID
        lockFile.writeAsStringSync('$pid\n');
        // 退出时清理锁文件
        ProcessSignal.sigint.watch().listen((_) {
          try {
            lockFile.deleteSync();
          } catch (_) {}
          exit(0);
        });
      }
      tick('single instance check');

      // 初始化 MediaKit
      MediaKit.ensureInitialized();
      tick('mediaKit initialized');

      // 桌面平台初始化托盘和窗口管理
      if (_isDesktop()) {
        await DesktopTray.init();
      }
      tick('desktop tray initialized');

      // 初始化前台服务（Android 保活）
      await ForegroundServiceManager.init();
      tick('foreground service initialized');

      // 清理可能残留的 Hive 锁文件（应用被强杀/系统杀死后锁文件未释放会导致 openBox 永久卡住）
      await _cleanupStaleHiveLocks();
      tick('hive lock cleanup done');

      if (kReleaseMode) {
        await Hive.initFlutter('pfm');
      } else {
        await Hive.initFlutter('pfm_dev');
      }

      Hive.registerAdapters();
      tick('hive initialized');

      final ls = await LocalStorage.instance();
      GetIt.I.registerSingleton<LocalStorage>(ls);
      tick('local storage loaded');

      // 应用持久化的日志等级（initTalkerLogger 已按构建模式设置了默认值，
      // 这里用用户配置覆盖，实现日志等级可配置）
      final savedLogLevel = ls.logLevel;
      if (savedLogLevel != null) {
        setLogLevel(
          LogLevel.values.firstWhere(
            (l) => l.name == savedLogLevel,
            orElse: () => kDebugMode ? LogLevel.debug : LogLevel.info,
          ),
        );
      }

      // 初始化后台服务
      await BackgroundService.initialize();
      final bgService = await BackgroundService.instance();
      GetIt.I.registerSingleton<BackgroundService>(bgService);
      final transferBloc = TransferBloc(bgService)
        ..add(TransferUploadSubscribeEvent())
        ..add(TransferDownloadSubscribeEvent());
      GetIt.I.registerSingleton(transferBloc);
      tick('background service + transfer bloc ready');

      final recentFilesStorage = await RecentFilesStorage.instance();
      GetIt.I.registerSingleton<RecentFilesStorage>(recentFilesStorage);
      tick('recent files storage ready');

      // 根据登录状态设置初始路由
      if (ls.isLoggedIn) {
        approuter.go(HomeRoute().location);

        // 延迟触发相册自动备份（不阻塞启动）
        if (Platform.isAndroid || Platform.isIOS) {
          Future.delayed(const Duration(seconds: 3), () async {
            try {
              final albumBackup = await AlbumBackupService.instance();
              await albumBackup.autoTrigger();
            } catch (e) {
              getLogger().warning('Album auto-trigger failed: $e');
            }
          });
        }
      } else {
        approuter.go(LoginRoute().location);
      }
      tick('initial route decided');

      final pfmr = FmRepository();
      tick('fm repository created');

      // 全局错误挂钩：框架/平台层未捕获异常写入日志
      FlutterError.onError = (details) {
        FlutterError.presentError(details);
        getLogger().error(
          'FlutterError: ${details.exceptionAsString()}',
          details.exception,
          details.stack,
        );
      };
      PlatformDispatcher.instance.onError = (error, stack) {
        getLogger().error('PlatformDispatcher error: $error', error, stack);
        return true;
      };
      // 首帧渲染完成 = 原生闪屏消失
      WidgetsBinding.instance.addPostFrameCallback((_) {
        tick('first frame rendered (splash dismissed)');
      });
      runApp(App(fmRepository: pfmr));
    },
    (error, stack) {
      try {
        getLogger().error('Uncaught zone error: $error', error, stack);
      } catch (_) {
        // talker 尚未初始化时兜底输出
        debugPrint('Uncaught zone error: $error\n$stack');
      }
    },
  );
}

/// 判断是否为桌面平台
bool _isDesktop() {
  return Platform.isWindows || Platform.isMacOS || Platform.isLinux;
}

/// 清理残留的 Hive 锁文件
///
/// 应用被强杀或系统 OOM 杀死时，Hive 的 .lock 文件可能未释放，
/// 导致下次启动时 Hive.openBox() 永久阻塞（卡在 splash）。
/// 此函数在 Hive.initFlutter 之前删除所有残留锁文件。
Future<void> _cleanupStaleHiveLocks() async {
  try {
    final appDir = await getApplicationDocumentsDirectory();
    for (final dirName in ['pfm', 'pfm_dev']) {
      final hiveDir = Directory('${appDir.path}/$dirName');
      if (!await hiveDir.exists()) continue;

      await for (final entity in hiveDir.list()) {
        if (entity is File && entity.path.endsWith('.lock')) {
          try {
            await entity.delete();
          } catch (_) {}
        }
      }
    }
  } catch (_) {
    // 清理失败不影响正常启动
  }
}