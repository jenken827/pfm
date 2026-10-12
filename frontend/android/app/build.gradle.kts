import java.util.Properties

plugins {
    id("com.android.application")
    // ⚠️ 这里**故意不写** id("kotlin-android")：android.builtInKotlin=false 时
    // Flutter Gradle 插件会自己给本项目应用 KGP，手写反而会触发 Flutter 的
    // "applies the Kotlin Gradle Plugin" 警告（见 AGENTS.md §5）。
    // The Flutter Gradle Plugin must be applied after the Android Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val keystorePropertiesFile = rootProject.file("key.properties")
val keystoreProperties = Properties()
if (keystorePropertiesFile.exists()) {
    keystoreProperties.load(keystorePropertiesFile.inputStream())
}

android {
    namespace = "com.shifangyuan.pfm"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "com.shifangyuan.pfm"
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        create("release") {
            val storeFilePath = keystoreProperties["storeFile"] as? String
            keyAlias = keystoreProperties["keyAlias"] as? String
            keyPassword = keystoreProperties["keyPassword"] as? String
            storeFile = storeFilePath?.let { file(it) }
            storePassword = keystoreProperties["storePassword"] as? String
        }
    }

    buildTypes {
        release {
            signingConfig = if (keystorePropertiesFile.exists()) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
    // 通话录音备份走 SAF（ACTION_OPEN_DOCUMENT_TREE）读目录：
    // 录音目录属 media_rw 组、本应用不在该组，路径与 MediaStore 都拿不到，
    // 必须经 SAF 授权的 content:// 逐文件读取
    implementation("androidx.documentfile:documentfile:1.0.1")
}

// jvmTarget 必须显式钉在 17，和上面的 compileOptions 对齐（否则 javac / kotlinc 目标不一致会报
// "Inconsistent JVM Target Compatibility"）。AGP 9 已废 android {} 里的 kotlinOptions，改在最外层写。
kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
