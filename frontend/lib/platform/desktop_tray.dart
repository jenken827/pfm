import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';

/// 桌面端托盘服务实现（Windows/macOS/Linux）
class DesktopTray implements TrayListener {
  static final DesktopTray _instance = DesktopTray._();
  static DesktopTray get instance => _instance;

  DesktopTray._();

  /// 初始化托盘和窗口管理
  static Future<void> init() async {
    await _instance._initWindowManager();
    await _instance._initTray();
  }

  /// 初始化窗口管理器
  Future<void> _initWindowManager() async {
    await windowManager.ensureInitialized();

    const windowOptions = WindowOptions(
      size: Size(1280, 720),
      center: true,
      backgroundColor: Colors.transparent,
      skipTaskbar: false,
      titleBarStyle: TitleBarStyle.normal,
    );

    await windowManager.waitUntilReadyToShow(windowOptions, () async {
      await windowManager.show();
      await windowManager.focus();
      // 阻止窗口关闭，改为隐藏到托盘
      await windowManager.setPreventClose(true);
    });

    // 设置最小尺寸
    await windowManager.setMinimumSize(const Size(800, 600));
  }

  /// 初始化托盘图标
  Future<void> _initTray() async {
    // 根据平台选择图标格式
    final iconPath = Platform.isWindows
        ? 'assets/images/tray_icon.ico'
        : 'assets/images/logo.png';

    await trayManager.setIcon(iconPath);

    // 设置托盘提示
    await trayManager.setToolTip('PFM - Personal File Manager');

    // 设置右键菜单
    await _setContextMenu();

    // 注册托盘监听器
    trayManager.addListener(_instance);
  }

  /// 设置右键菜单
  Future<void> _setContextMenu() async {
    final menu = Menu(
      items: [
        MenuItem(
          key: 'show_window',
          label: '显示主窗口',
        ),
        MenuItem.separator(),
        MenuItem(
          key: 'exit',
          label: '退出',
        ),
      ],
    );
    await trayManager.setContextMenu(menu);
  }

  /// 显示窗口
  static Future<void> showWindow() async {
    await windowManager.show();
    await windowManager.focus();
  }

  /// 隐藏窗口到托盘
  static Future<void> hideToTray() async {
    await windowManager.hide();
  }

  /// 销毁托盘
  static Future<void> destroy() async {
    trayManager.removeListener(_instance);
    await trayManager.destroy();
  }

  // === TrayListener 回调 ===

  @override
  void onTrayIconMouseDown() {
    // 单击托盘图标 - 显示窗口
    showWindow();
  }

  @override
  void onTrayIconMouseUp() {
    // 鼠标释放（Windows 不需要处理）
  }

  @override
  void onTrayIconRightMouseDown() {
    // 右键点击 - 显示菜单（macOS/Windows 自动处理）
    trayManager.popUpContextMenu();
  }

  @override
  void onTrayIconRightMouseUp() {
    // 右键释放（Windows 不需要处理）
  }

  @override
  Future<void> onTrayMenuItemClick(MenuItem menuItem) async {
    switch (menuItem.key) {
      case 'show_window':
        await showWindow();
        break;
      case 'exit':
        // 真正退出应用
        await destroy();
        SystemNavigator.pop();
        break;
    }
  }
}
