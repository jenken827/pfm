import 'dart:io';

import 'package:flutter_background_service/flutter_background_service.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:pfm/services/logger.dart';

/// 前台服务管理 —— 有上传/下载任务时保活，完成后自动停止
class ForegroundServiceManager {
  static bool _initialized = false;
  static bool _running = false;

  /// 初始化前台服务（应用启动时调用一次）
  static Future<void> init() async {
    if (_initialized) return;
    if (!Platform.isAndroid) return;

    // 先创建通知渠道（Android 8+ 必须）
    await _createNotificationChannel();

    final service = FlutterBackgroundService();

    await service.configure(
      androidConfiguration: AndroidConfiguration(
        notificationChannelId: 'pfm_transfer',
        initialNotificationTitle: 'PFM 文件传输',
        initialNotificationContent: '正在传输文件...',
        autoStart: false,
        isForegroundMode: true,
        onStart: _onStart,
      ),
      iosConfiguration: IosConfiguration(
        autoStart: false,
        onForeground: _onStart,
        onBackground: (_) => true,
      ),
    );

    _initialized = true;
    getLogger().info('ForegroundService initialized');
  }

  /// 创建通知渠道
  static Future<void> _createNotificationChannel() async {
    final notifications = FlutterLocalNotificationsPlugin();
    const android = AndroidInitializationSettings('@mipmap/ic_launcher');
    await notifications.initialize(
      const InitializationSettings(android: android),
    );

    // 创建通知渠道
    const channel = AndroidNotificationChannel(
      'pfm_transfer',
      'PFM 文件传输',
      description: '文件上传下载进度通知',
      importance: Importance.low,
    );
    await notifications
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >()
        ?.createNotificationChannel(channel);
  }

  /// 启动前台服务（有活跃任务时调用）
  static Future<void> start() async {
    if (!Platform.isAndroid || _running) return;

    try {
      final service = FlutterBackgroundService();
      await service.startService();
      _running = true;
      getLogger().info('ForegroundService started');
    } catch (e) {
      getLogger().error('ForegroundService start failed', e);
    }
  }

  /// 停止前台服务（所有任务完成时调用）
  static Future<void> stop() async {
    if (!Platform.isAndroid || !_running) return;

    try {
      final service = FlutterBackgroundService();
      service.invoke('stop');
      _running = false;
      getLogger().info('ForegroundService stopped');
    } catch (e) {
      getLogger().error('ForegroundService stop failed', e);
    }
  }

  /// 更新通知内容
  static Future<void> updateNotification({
    int? uploading,
    int? downloading,
    int? completed,
    int? total,
  }) async {
    if (!Platform.isAndroid || !_running) return;

    try {
      final service = FlutterBackgroundService();
      String title = 'PFM 文件传输';
      String body = '';

      if (uploading != null && uploading > 0) {
        body = '正在上传 $uploading 个文件';
      }
      if (downloading != null && downloading > 0) {
        if (body.isNotEmpty) body += '，';
        body += '正在下载 $downloading 个文件';
      }
      if (completed != null && total != null && total > 0) {
        body += ' ($completed/$total 已完成)';
      }
      if (body.isEmpty) {
        body = '正在传输文件...';
      }

      service.invoke('update', {'title': title, 'body': body});
    } catch (e) {
      // 通知更新失败不影响传输
    }
  }

  static bool get isRunning => _running;
}

/// 前台服务入口点
@pragma('vm:entry-point')
void _onStart(ServiceInstance service) {
  if (service is AndroidServiceInstance) {
    service.on('update').listen((event) {
      if (event != null) {
        service.setForegroundNotificationInfo(
          title: event['title'] ?? 'PFM 文件传输',
          content: event['body'] ?? '正在传输文件...',
        );
      }
    });

    service.on('stop').listen((event) {
      service.stopSelf();
    });
  }
}
