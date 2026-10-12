pluginManagement {
    val flutterSdkPath =
        run {
            val properties = java.util.Properties()
            file("local.properties").inputStream().use { properties.load(it) }
            val flutterSdkPath = properties.getProperty("flutter.sdk")
            require(flutterSdkPath != null) { "flutter.sdk not set in local.properties" }
            flutterSdkPath
        }

    includeBuild("$flutterSdkPath/packages/flutter_tools/gradle")

    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

plugins {
    id("dev.flutter.flutter-plugin-loader") version "1.0.0"
    // 与其它 Flutter 项目保持一致（= Flutter 3.47 模板的默认值，见 AGENTS.md §5）
    id("com.android.application") version "9.1.0" apply false
    id("org.jetbrains.kotlin.android") version "2.4.0" apply false
}

include(":app")

// ---------------------------------------------------------------------------
// 这里曾经有一段 gradle.beforeProject { ... }：给「只声明 com.android.library、
// 却带 .kt 源码、又没应用 KGP」的插件子工程（当时的 flutter_contacts）兜底套上 KGP。
//
// Flutter 3.47 起 Flutter Gradle 插件自己会做这件事
// （FlutterPluginUtils.detectApplyingKotlinGradlePlugin：凡有 AGP app/library 插件、
//  但 build 脚本文本里没声明 KGP 的子工程，自动 pluginManager.apply("kotlin-android")），
// 兜底已多余 —— 已删除，请不要再加回来（AGP 9 + builtInKotlin=false 下重复应用没有意义）。
// ---------------------------------------------------------------------------
