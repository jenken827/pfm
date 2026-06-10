import 'package:flutter/material.dart';
import 'package:flutter/cupertino.dart';
import 'package:pfm/design/design_tokens.dart';

/// PFM 应用主题
/// 使用自定义设计系统，支持 Light/Dark 模式
final class AppTheme {
  AppTheme({required this.themeName});
  final String themeName;

  ThemeData light() {
    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.light,
      colorScheme: ColorScheme.light(
        primary: PfmColors.primary,
        onPrimary: Colors.white,
        primaryContainer: PfmColors.primarySurface,
        onPrimaryContainer: PfmColors.primaryDark,
        secondary: PfmColors.primaryLight,
        surface: PfmColors.lightSurface,
        onSurface: PfmColors.lightTextPrimary,
        onSurfaceVariant: PfmColors.lightTextSecondary,
        outline: PfmColors.lightBorder,
        outlineVariant: PfmColors.lightBorder.withValues(alpha: 0.5),
        error: PfmColors.error,
        onError: Colors.white,
      ),
      scaffoldBackgroundColor: PfmColors.lightBackground,
      cardTheme: CardThemeData(
        color: PfmColors.lightSurface,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.lg),
          side: const BorderSide(color: PfmColors.lightBorder),
        ),
        margin: EdgeInsets.zero,
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: PfmColors.lightSurface,
        foregroundColor: PfmColors.lightTextPrimary,
        elevation: 0,
        scrolledUnderElevation: 1,
        titleTextStyle: PfmTypography.titleLarge.copyWith(color: PfmColors.lightTextPrimary),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: PfmColors.lightSurface,
        indicatorColor: PfmColors.primarySurface,
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return PfmTypography.labelMedium.copyWith(color: PfmColors.primary);
          }
          return PfmTypography.labelMedium.copyWith(color: PfmColors.lightTextSecondary);
        }),
        iconTheme: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return const IconThemeData(color: PfmColors.primary, size: 24);
          }
          return IconThemeData(color: PfmColors.lightTextSecondary, size: 24);
        }),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: PfmColors.lightSurface,
        indicatorColor: PfmColors.primarySurface,
        selectedIconTheme: const IconThemeData(color: PfmColors.primary, size: 24),
        unselectedIconTheme: IconThemeData(color: PfmColors.lightTextSecondary, size: 24),
        selectedLabelTextStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.primary),
        unselectedLabelTextStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.lightTextSecondary),
      ),
      dividerTheme: const DividerThemeData(
        color: PfmColors.lightBorder,
        thickness: 1,
        space: 1,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: PfmColors.lightSurface,
        contentPadding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.lightBorder),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.lightBorder),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.primary, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.error),
        ),
        hintStyle: PfmTypography.bodyMedium.copyWith(color: PfmColors.lightTextMuted),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: PfmColors.primary,
          foregroundColor: Colors.white,
          elevation: 0,
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.md),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: PfmColors.lightTextPrimary,
          side: const BorderSide(color: PfmColors.lightBorder),
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.md),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: PfmColors.lightTextSecondary,
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.md, vertical: PfmSpacing.sm),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.sm),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.xs),
        titleTextStyle: PfmTypography.bodyLarge.copyWith(color: PfmColors.lightTextPrimary),
        subtitleTextStyle: PfmTypography.bodyMedium.copyWith(color: PfmColors.lightTextSecondary),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: PfmColors.lightSurface,
        elevation: 8,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.xl),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: PfmColors.lightTextPrimary,
        contentTextStyle: PfmTypography.bodyMedium.copyWith(color: Colors.white),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
        ),
        behavior: SnackBarBehavior.floating,
      ),
      chipTheme: ChipThemeData(
        backgroundColor: PfmColors.lightSurfaceHover,
        selectedColor: PfmColors.primarySurface,
        side: const BorderSide(color: PfmColors.lightBorder),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.full),
        ),
        labelStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.lightTextPrimary),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: PfmColors.lightSurface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(PfmRadius.xl)),
        ),
      ),
    );
  }

  ThemeData dark() {
    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.dark,
      colorScheme: ColorScheme.dark(
        primary: PfmColors.primaryLight,
        onPrimary: Colors.white,
        primaryContainer: PfmColors.primaryDark,
        onPrimaryContainer: PfmColors.primarySurface,
        secondary: PfmColors.primary,
        surface: PfmColors.darkSurface,
        onSurface: PfmColors.darkTextPrimary,
        onSurfaceVariant: PfmColors.darkTextSecondary,
        outline: PfmColors.darkBorder,
        outlineVariant: PfmColors.darkBorder.withValues(alpha: 0.5),
        error: PfmColors.error,
        onError: Colors.white,
      ),
      scaffoldBackgroundColor: PfmColors.darkBackground,
      cardTheme: CardThemeData(
        color: PfmColors.darkSurface,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.lg),
          side: const BorderSide(color: PfmColors.darkBorder),
        ),
        margin: EdgeInsets.zero,
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: PfmColors.darkSurface,
        foregroundColor: PfmColors.darkTextPrimary,
        elevation: 0,
        scrolledUnderElevation: 1,
        titleTextStyle: PfmTypography.titleLarge.copyWith(color: PfmColors.darkTextPrimary),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: PfmColors.darkSurface,
        indicatorColor: PfmColors.primaryDark,
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return PfmTypography.labelMedium.copyWith(color: PfmColors.primaryLight);
          }
          return PfmTypography.labelMedium.copyWith(color: PfmColors.darkTextSecondary);
        }),
        iconTheme: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return const IconThemeData(color: PfmColors.primaryLight, size: 24);
          }
          return IconThemeData(color: PfmColors.darkTextSecondary, size: 24);
        }),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: PfmColors.darkSurface,
        indicatorColor: PfmColors.primaryDark,
        selectedIconTheme: const IconThemeData(color: PfmColors.primaryLight, size: 24),
        unselectedIconTheme: IconThemeData(color: PfmColors.darkTextSecondary, size: 24),
        selectedLabelTextStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.primaryLight),
        unselectedLabelTextStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.darkTextSecondary),
      ),
      dividerTheme: const DividerThemeData(
        color: PfmColors.darkBorder,
        thickness: 1,
        space: 1,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: PfmColors.darkSurface,
        contentPadding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.darkBorder),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.darkBorder),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.primaryLight, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
          borderSide: const BorderSide(color: PfmColors.error),
        ),
        hintStyle: PfmTypography.bodyMedium.copyWith(color: PfmColors.darkTextMuted),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: PfmColors.primaryLight,
          foregroundColor: Colors.white,
          elevation: 0,
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.md),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: PfmColors.darkTextPrimary,
          side: const BorderSide(color: PfmColors.darkBorder),
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.md),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.md),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: PfmColors.darkTextSecondary,
          padding: const EdgeInsets.symmetric(horizontal: PfmSpacing.md, vertical: PfmSpacing.sm),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(PfmRadius.sm),
          ),
          textStyle: PfmTypography.labelLarge,
        ),
      ),
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(horizontal: PfmSpacing.lg, vertical: PfmSpacing.xs),
        titleTextStyle: PfmTypography.bodyLarge.copyWith(color: PfmColors.darkTextPrimary),
        subtitleTextStyle: PfmTypography.bodyMedium.copyWith(color: PfmColors.darkTextSecondary),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: PfmColors.darkSurface,
        elevation: 8,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.xl),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: PfmColors.darkSurface,
        contentTextStyle: PfmTypography.bodyMedium.copyWith(color: PfmColors.darkTextPrimary),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.md),
        ),
        behavior: SnackBarBehavior.floating,
      ),
      chipTheme: ChipThemeData(
        backgroundColor: PfmColors.darkSurfaceHover,
        selectedColor: PfmColors.primaryDark,
        side: const BorderSide(color: PfmColors.darkBorder),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(PfmRadius.full),
        ),
        labelStyle: PfmTypography.labelMedium.copyWith(color: PfmColors.darkTextPrimary),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: PfmColors.darkSurface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(PfmRadius.xl)),
        ),
      ),
    );
  }
}
