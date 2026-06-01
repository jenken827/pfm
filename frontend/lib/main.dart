import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:hive_ce_flutter/hive_ce_flutter.dart';
import 'package:media_kit/media_kit.dart';
import 'package:pfm/api/api.dart';
import 'package:pfm/blocs/blocs.dart';
import 'package:pfm/hive/hive.dart';
import 'package:pfm/platform/platform.dart';
import 'services/services.dart';
import 'router/router.dart';
import 'app.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // 初始化 MediaKit
  MediaKit.ensureInitialized();

  // 桌面平台初始化托盘和窗口管理
  if (_isDesktop()) {
    await DesktopTray.init();
  }

  await initTalkerLogger();
  
  // 初始化前台服务（Android 保活）
  await ForegroundServiceManager.init();
  
  if (kReleaseMode) {
    await Hive.initFlutter('pfm');
  } else {
    await Hive.initFlutter('pfm_dev');
  }

  Hive.registerAdapters();

  final ls = await LocalStorage.instance();
  GetIt.I.registerSingleton<LocalStorage>(ls);

  // 初始化后台服务
  await BackgroundService.initialize();
  final bgService = await BackgroundService.instance();
  GetIt.I.registerSingleton<BackgroundService>(bgService);
  final transferBloc = TransferBloc(bgService)
    ..add(TransferUploadSubscribeEvent())
    ..add(TransferDownloadSubscribeEvent());
  GetIt.I.registerSingleton(transferBloc);

  // 根据登录状态设置初始路由
  if (ls.isLoggedIn) {
    approuter.go(HomeRoute().location);
  } else {
    approuter.go(LoginRoute().location);
  }

  final pfmr = FmRepository();

  runApp(App(fmRepository: pfmr));
}

/// 判断是否为桌面平台
bool _isDesktop() {
  return Platform.isWindows || Platform.isMacOS || Platform.isLinux;
}
