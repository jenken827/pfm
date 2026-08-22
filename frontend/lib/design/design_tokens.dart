import 'package:flutter/material.dart';

/// PFM 设计系统 - Design Tokens
/// 基于 Clean Minimal 设计方案
/// 所有设计相关的常量都集中管理

// ============================================
// 颜色系统
// ============================================

/// 品牌色
class PfmColors {
  PfmColors._();

  // 主色调 - 蓝色系
  static const Color primary = Color(0xFF2563EB);
  static const Color primaryLight = Color(0xFF3B82F6);
  static const Color primaryDark = Color(0xFF1D4ED8);
  static const Color primarySurface = Color(0xFFEFF6FF);
  static const Color primaryBorder = Color(0xFFBFDBFE);

  // 语义色
  static const Color success = Color(0xFF10B981);
  static const Color successSurface = Color(0xFFD1FAE5);
  static const Color warning = Color(0xFFF59E0B);
  static const Color warningSurface = Color(0xFFFEF3C7);
  static const Color error = Color(0xFFEF4444);
  static const Color errorSurface = Color(0xFFFEE2E2);
  static const Color info = Color(0xFF3B82F6);
  static const Color infoSurface = Color(0xFFDBEAFE);

  // 中性色 - 浅色模式
  static const Color lightBackground = Color(0xFFFAFAFA);
  static const Color lightSurface = Color(0xFFFFFFFF);
  static const Color lightSurfaceHover = Color(0xFFF5F5F5);
  static const Color lightBorder = Color(0xFFE8E8E8);
  static const Color lightTextPrimary = Color(0xFF1A1A1A);
  static const Color lightTextSecondary = Color(0xFF666666);
  static const Color lightTextMuted = Color(0xFF999999);

  // 中性色 - 深色模式
  static const Color darkBackground = Color(0xFF0F0F0F);
  static const Color darkSurface = Color(0xFF1A1A1A);
  static const Color darkSurfaceHover = Color(0xFF2A2A2A);
  static const Color darkBorder = Color(0xFF2A2A2A);
  static const Color darkTextPrimary = Color(0xFFE8E8E8);
  static const Color darkTextSecondary = Color(0xFFA0A0A0);
  static const Color darkTextMuted = Color(0xFF666666);

  // 文件类型色
  static const Color folder = Color(0xFF2563EB);
  static const Color folderSurface = Color(0xFFDBEAFE);
  static const Color image = Color(0xFFDB2777);
  static const Color imageSurface = Color(0xFFFCE7F3);
  static const Color document = Color(0xFF059669);
  static const Color documentSurface = Color(0xFFD1FAE5);
  static const Color video = Color(0xFF7C3AED);
  static const Color videoSurface = Color(0xFFEDE9FE);
  static const Color archive = Color(0xFFD97706);
  static const Color archiveSurface = Color(0xFFFEF3C7);
}

// ============================================
// 间距系统
// ============================================

class PfmSpacing {
  PfmSpacing._();

  static const double xs = 4.0;
  static const double sm = 8.0;
  static const double md = 12.0;
  static const double lg = 16.0;
  static const double xl = 20.0;
  static const double xxl = 24.0;
  static const double xxxl = 32.0;
  static const double xxxxl = 40.0;
}

// ============================================
// 圆角系统
// ============================================

class PfmRadius {
  PfmRadius._();

  static const double xs = 4.0;
  static const double sm = 6.0;
  static const double md = 8.0;
  static const double lg = 12.0;
  static const double xl = 16.0;
  static const double full = 999.0;
}

// ============================================
// 阴影系统
// ============================================

class PfmShadows {
  PfmShadows._();

  static const List<BoxShadow> sm = [
    BoxShadow(color: Color(0x0A000000), blurRadius: 2, offset: Offset(0, 1)),
  ];

  static const List<BoxShadow> md = [
    BoxShadow(color: Color(0x0F000000), blurRadius: 12, offset: Offset(0, 4)),
  ];

  static const List<BoxShadow> lg = [
    BoxShadow(color: Color(0x14000000), blurRadius: 24, offset: Offset(0, 8)),
  ];
}

// ============================================
// 排版系统
// ============================================

class PfmTypography {
  PfmTypography._();

  // 全局字体：Noto Sans SC（OFL 开源，中英文统一字形）。
  // 避免 Windows 默认字体（Segoe UI + 系统中文回退）导致的中文粗细/颜色深浅不一致，
  // 打包进安装包，桌面端与移动端渲染一致。
  static const String fontFamily = 'NotoSansSC';

  // 大标题
  static const TextStyle displayLarge = TextStyle(
    fontFamily: fontFamily,
    fontSize: 28,
    fontWeight: FontWeight.w700,
    letterSpacing: -0.5,
    height: 1.2,
  );

  // 页面标题
  static const TextStyle displayMedium = TextStyle(
    fontFamily: fontFamily,
    fontSize: 24,
    fontWeight: FontWeight.w700,
    letterSpacing: -0.3,
    height: 1.3,
  );

  // 区域标题
  static const TextStyle titleLarge = TextStyle(
    fontFamily: fontFamily,
    fontSize: 18,
    fontWeight: FontWeight.w600,
    letterSpacing: -0.2,
    height: 1.4,
  );

  // 卡片标题
  static const TextStyle titleMedium = TextStyle(
    fontFamily: fontFamily,
    fontSize: 16,
    fontWeight: FontWeight.w600,
    height: 1.4,
  );

  // 小标题
  static const TextStyle titleSmall = TextStyle(
    fontFamily: fontFamily,
    fontSize: 14,
    fontWeight: FontWeight.w600,
    height: 1.4,
  );

  // 正文
  static const TextStyle bodyLarge = TextStyle(
    fontFamily: fontFamily,
    fontSize: 14,
    fontWeight: FontWeight.w400,
    height: 1.5,
  );

  // 正文小
  static const TextStyle bodyMedium = TextStyle(
    fontFamily: fontFamily,
    fontSize: 13,
    fontWeight: FontWeight.w400,
    height: 1.5,
  );

  // 正文更小
  static const TextStyle bodySmall = TextStyle(
    fontFamily: fontFamily,
    fontSize: 12,
    fontWeight: FontWeight.w400,
    height: 1.5,
  );

  // 标签
  static const TextStyle labelLarge = TextStyle(
    fontFamily: fontFamily,
    fontSize: 13,
    fontWeight: FontWeight.w500,
    height: 1.4,
  );

  // 小标签
  static const TextStyle labelMedium = TextStyle(
    fontFamily: fontFamily,
    fontSize: 12,
    fontWeight: FontWeight.w500,
    height: 1.4,
  );

  // 辅助文字
  static const TextStyle labelSmall = TextStyle(
    fontFamily: fontFamily,
    fontSize: 11,
    fontWeight: FontWeight.w500,
    height: 1.4,
  );

  // 统计数字
  static const TextStyle statValue = TextStyle(
    fontFamily: fontFamily,
    fontSize: 28,
    fontWeight: FontWeight.w700,
    letterSpacing: -0.5,
    height: 1.2,
  );

  // 统计标签
  static const TextStyle statLabel = TextStyle(
    fontFamily: fontFamily,
    fontSize: 12,
    fontWeight: FontWeight.w500,
    letterSpacing: 0.3,
    height: 1.4,
  );
}

// ============================================
// 动画时长
// ============================================

class PfmDuration {
  PfmDuration._();

  static const Duration fast = Duration(milliseconds: 100);
  static const Duration normal = Duration(milliseconds: 200);
  static const Duration slow = Duration(milliseconds: 300);
}

// ============================================
// 尺寸约束
// ============================================

class PfmConstraints {
  PfmConstraints._();

  // 导航栏
  static const double navRailWidth = 220.0;
  static const double navRailWidthCompact = 72.0;
  static const double bottomNavHeight = 64.0;

  // 卡片
  static const double cardMinWidth = 180.0;
  static const double statCardMinWidth = 160.0;

  // 文件网格
  static const double fileGridMinWidth = 160.0;
  static const double fileGridMaxWidth = 220.0;

  // 响应式断点
  static const double mobileBreakpoint = 600.0;
  static const double tabletBreakpoint = 1024.0;
  static const double desktopBreakpoint = 1440.0;
}
