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
    id("com.android.application") version "8.11.1" apply false
    id("org.jetbrains.kotlin.android") version "2.2.20" apply false
}

include(":app")

// ---------------------------------------------------------------------------
// 给「只声明 android.library、却带 Kotlin 源码」的插件子工程补上 KGP。
//
// 背景（实测，2026-09-21）：
// flutter_contacts 2.5.0 的 android/build.gradle 里只有
//     apply plugin: 'com.android.library'
//     sourceSets { main.java.srcDirs += 'src/main/kotlin' }
//     kotlin { compilerOptions { jvmTarget = ... } }
// 即「声明了 Kotlin 源目录与 kotlin 扩展，却**没有**应用 Kotlin 插件」。
// 其 .kt 源码因此不被编译，生成的 GeneratedPluginRegistrant.java 引用
// co.quis.flutter_contacts.FlutterContactsPlugin 时报「找不到符号」。
//
// Flutter 的插件加载器（native_plugin_loader.gradle.kts）只负责 include
// 子工程，不会替它们应用 KGP；插件必须自己声明。上游尚未迁移，这里兜底。
//
// 用 gradle.beforeProject（每工程评估前触发）：实测 projectsLoaded 时机不对，
// 那时 Flutter 的插件工程尚未 include 完，遍历不到。beforeProject 逐个命中，
// 已在 :flutter_contacts 上验证 compileDebugKotlin 确实进入了任务图。
// 只对「确有 .kt 源码、且 build 脚本未声明 KGP」的子工程生效，:app 不受影响。
// ---------------------------------------------------------------------------
// 注意（实测坑）：曾出现 :flutter_contacts:compileDebugKotlin 报 UP-TO-DATE
// 但产物缺 4 个包 + 顶层 FlutterContactsPlugin.class，导致 app 的 javac 报
// 「找不到符号」。根因是首次运行（那时插件尚未被注入 KGP）留下的坏增量状态，
// 被后续构建信任。处置：`gradlew :flutter_contacts:compileDebugKotlin --rerun-tasks`
// 强制重编即可恢复（实测 244 → 293 个类）。若再遇到，先删 build/<plugin> 并 rerun。
gradle.beforeProject {
    val proj = this
    if (proj.path == ":app") return@beforeProject
    // 根工程（path 为 ":"）也没有 Kotlin 源码，跳过
    if (proj.path == ":") return@beforeProject

    val projectDir: java.io.File = proj.projectDir
    var hasKotlinSources = false
    for (rel in arrayOf("src/main/kotlin", "src/main/java")) {
        val dir = java.io.File(projectDir, rel)
        if (!dir.isDirectory) continue
        dir.walkTopDown().forEach { f ->
            if (f.isFile && f.extension == "kt") hasKotlinSources = true
        }
        if (hasKotlinSources) break
    }
    if (!hasKotlinSources) return@beforeProject

    var buildScript: java.io.File? = null
    for (n in arrayOf("build.gradle", "build.gradle.kts")) {
        val f = java.io.File(projectDir, n)
        if (f.isFile) { buildScript = f; break }
    }
    if (buildScript == null) return@beforeProject

    val declaresKgp = Regex("(kotlin-android|org\\.jetbrains\\.kotlin\\.android)")
        .containsMatchIn(buildScript.readText())
    if (declaresKgp) return@beforeProject

    proj.pluginManager.apply("org.jetbrains.kotlin.android")
}
