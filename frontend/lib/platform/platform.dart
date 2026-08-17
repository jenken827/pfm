/// 平台条件导出
/// 桌面平台（Windows/macOS/Linux）使用实际实现
/// 移动平台（Android/iOS）使用空实现
library;

export 'desktop_stub.dart' if (dart.library.io) 'desktop_tray.dart';
