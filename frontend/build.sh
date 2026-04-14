#!/usr/bin/bash

set -e

# 显示帮助信息
show_help() {
    cat << EOF
用法: $0 [选项] [平台]

选项:
  icon         执行 flutter_launcher_icons 图标生成
  splash       执行 flutter_native_splash 启动页生成
  build_runner 执行 build_runner 代码生成
  build        执行 Flutter 构建（会自动更新构建号）
  all          执行所有任务（默认行为）
  help         显示此帮助信息

平台 (仅用于 build 和 all 命令):
  android      构建 Android APK
  ios          构建 iOS
  windows      构建 Windows 应用
  macos        构建 macOS 应用
  linux        构建 Linux 应用
  web          构建 Web 应用
  apk          Android APK 的别名
  appbundle    构建 Android App Bundle

示例:
  $0 icon                    # 只生成应用图标
  $0 splash                  # 只生成启动页
  $0 build_runner            # 只运行代码生成
  $0 build android           # 只构建 Android APK
  $0 build windows           # 只构建 Windows 应用
  $0 build apk               # 构建Android APK（apk是android的别名）
  $0 all android             # 执行所有任务并构建 Android APK
  $0 all                     # 执行所有任务（默认构建 Android APK）
  $0                         # 默认执行所有任务

注意: 
  - 只有在执行 'build' 或 'all' 命令时才会自动更新构建号
  - 如果不指定平台，默认构建 Android APK
EOF
}

# 检查必要的命令是否存在
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo "❌ 错误: 命令 '$1' 未找到，请安装后重试"
        exit 1
    fi
}

# 检查Flutter和Dart命令
check_flutter_commands() {
    if ! command -v puro &> /dev/null; then
        echo "❌ 错误: puro命令未找到，请确保Puro已正确安装并配置PATH"
        exit 1
    fi

    # 验证 puro flutter 和 puro dart 是否可用
    if ! puro flutter --version &> /dev/null; then
        echo "❌ 错误: puro flutter 不可用，请确保Puro已正确配置Flutter"
        exit 1
    fi

    if ! puro dart --version &> /dev/null; then
        echo "❌ 错误: puro dart 不可用，请确保Puro已正确配置Dart"
        exit 1
    fi
}

# 更新版本构建号
update_build_number() {
    echo "🔧 开始更新构建号..."
    
    # 检查sed命令
    if [[ "$OSTYPE" == "darwin"* ]]; then
        if command -v gsed &> /dev/null; then
            SED_CMD="gsed"
        else
            SED_CMD="sed"
            check_command "sed"
        fi
    else
        SED_CMD="sed"
        check_command "sed"
    fi

    # 检查其他必要命令
    check_command "date"
    check_command "grep"
    check_command "cp"
    check_command "rm"
    # 检查git命令
    check_command "git"

    # 检查pubspec.yaml文件是否存在
    if [[ ! -f "pubspec.yaml" ]]; then
        echo "❌ 错误: pubspec.yaml文件未找到"
        exit 1
    fi

    # 获取当前版本信息
    if ! CURRENT_VERSION_LINE=$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+(\+[0-9a-zA-Z_\.+-]+)?$' pubspec.yaml); then
        echo "❌ 错误: 无法从pubspec.yaml中读取版本号"
        exit 1
    fi

    # 提取主版本号和构建号
    CURRENT_VERSION=$(echo "$CURRENT_VERSION_LINE" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
    CURRENT_BUILD=$(echo "$CURRENT_VERSION_LINE" | grep -oE '\+[0-9a-zA-Z_\.+-]+' | sed 's/\+//' || echo "")

    # 获取当前日期，格式为YYYYMMDD
    CURRENT_DATE=$(date +%Y%m%d)
    # 获取当前commit hash的前8位
    COMMIT_HASH=$(git rev-parse --short=8 HEAD)
    # 构建新的构建号：日期 + commit hash（用于versionName）
    NEW_BUILD_NUMBER="${CURRENT_DATE}-${COMMIT_HASH}"
    echo "当前版本: $CURRENT_VERSION, 当前构建号: $CURRENT_BUILD, 新构建号: $NEW_BUILD_NUMBER"

    # 备份原始pubspec.yaml文件
    cp pubspec.yaml pubspec.yaml.backup
    echo "✅ 已备份pubspec.yaml文件"

    # 构建新的版本字符串 - 使用 only the date as the version code part (for Android compatibility)
    # The version format will be: MAJOR.MINOR.PATCH+VERSION_CODE (where VERSION_CODE is just the date)
    # But we'll store the full build info in a way that Flutter can handle
    NEW_VERSION="$CURRENT_VERSION+$CURRENT_DATE"

    # 使用适当的sed命令进行替换
    if [[ "$OSTYPE" == "darwin"* ]]; then
        if [[ "$SED_CMD" == "gsed" ]]; then
            $SED_CMD -i "s/version: $CURRENT_VERSION\(\+[0-9a-zA-Z_\.+-]*\)\?/version: $NEW_VERSION/" pubspec.yaml
        else
            $SED_CMD -i '' "s/version: $CURRENT_VERSION\(\+[0-9a-zA-Z_\.+-]*\)\?/version: $NEW_VERSION/" pubspec.yaml
        fi
    else
        $SED_CMD -i "s/version: $CURRENT_VERSION\(\+[0-9a-zA-Z_\.+-]*\)\?/version: $NEW_VERSION/" pubspec.yaml
    fi

    # 验证更新是否成功
    if grep -q "version: $NEW_VERSION" pubspec.yaml; then
        echo "✅ 版本构建号已成功更新为: $NEW_VERSION"
        echo "ℹ️  注意: Android versionCode使用日期 $CURRENT_DATE，完整构建信息为 $NEW_BUILD_NUMBER"
    else
        echo "❌ 版本构建号更新失败，恢复备份文件"
        cp pubspec.yaml.backup pubspec.yaml
        rm pubspec.yaml.backup
        exit 1
    fi
}

# 恢复构建号（在构建完成后）
restore_build_number() {
    if [[ -f "pubspec.yaml.backup" ]]; then
        echo "🔄 恢复原始构建号..."
        cp pubspec.yaml.backup pubspec.yaml
        rm pubspec.yaml.backup
        echo "✅ 构建号已恢复"
    fi
}

# 替换about页面中的commit hash占位符
replace_commit_hash_in_about() {
    ABOUT_FILE="lib/pages/settings/about_page.dart"
    if [[ ! -f "$ABOUT_FILE" ]]; then
        echo "⚠️  警告: about页面文件未找到: $ABOUT_FILE"
        return 0
    fi
    
    # 获取当前commit hash的前8位
    COMMIT_HASH=$(git rev-parse --short=8 HEAD)
    
    # 备份about页面文件
    cp "$ABOUT_FILE" "$ABOUT_FILE.backup"
    echo "✅ 已备份about页面文件"
    
    # 替换{{commit-hash}}占位符
    if [[ "$OSTYPE" == "darwin"* ]]; then
        if command -v gsed &> /dev/null; then
            gsed -i "s/{{commit-hash}}/$COMMIT_HASH/g" "$ABOUT_FILE"
        else
            sed -i '' "s/{{commit-hash}}/$COMMIT_HASH/g" "$ABOUT_FILE"
        fi
    else
        sed -i "s/{{commit-hash}}/$COMMIT_HASH/g" "$ABOUT_FILE"
    fi
    
    echo "✅ About页面中的commit hash已替换为: $COMMIT_HASH"
}

# 恢复about页面中的commit hash占位符
restore_commit_hash_in_about() {
    ABOUT_FILE="lib/pages/settings_about/about_page.dart"
    if [[ -f "$ABOUT_FILE.backup" ]]; then
        echo "🔄 恢复about页面中的commit hash占位符..."
        cp "$ABOUT_FILE.backup" "$ABOUT_FILE"
        rm "$ABOUT_FILE.backup"
        echo "✅ About页面已恢复"
    fi
}

# 生成应用图标
generate_icons() {
    echo "🎨 生成应用图标..."
    puro dart run flutter_launcher_icons
    echo "✅ 应用图标生成完成"
}

# 生成启动页
generate_splash() {
    echo "🌅 生成启动页..."
    puro dart run flutter_native_splash:create
    echo "✅ 启动页生成完成"
}

# 运行代码生成
run_build_runner() {
    echo "🔨 运行代码生成..."
    puro dart run build_runner build --delete-conflicting-outputs
    echo "✅ 代码生成完成"
}

# 执行Flutter构建
run_flutter_build() {
    local PLATFORM="${1:-android}"  # 默认构建 android
    
    echo "🏗️  执行Flutter构建..."
    echo "🎯 目标平台: $PLATFORM"
    
    # 获取应用名称、版本号和构建号
    APP_NAME=$(grep -E '^name: ' pubspec.yaml | sed 's/name: //' | tr -d ' ')
    VERSION_INFO=$(grep -E '^version: ' pubspec.yaml | sed 's/version: //')
    VERSION=$(echo "$VERSION_INFO" | cut -d'+' -f1)
    BUILD_NUMBER_DATE=$(echo "$VERSION_INFO" | cut -d'+' -f2)
    
    # 获取当前commit hash的前8位 for filename
    COMMIT_HASH=$(git rev-parse --short=8 HEAD)
    BUILD_NUMBER="${BUILD_NUMBER_DATE}-${COMMIT_HASH}"
    
    echo "应用名称: $APP_NAME"
    echo "版本号: $VERSION"
    echo "构建号: $BUILD_NUMBER"
    
    # 根据平台执行不同的构建命令
    case "$PLATFORM" in
        android|apk)
            build_android_apk "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        appbundle)
            build_android_appbundle "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        ios)
            build_ios "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        windows)
            build_windows "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        macos)
            build_macos "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        linux)
            build_linux "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        web)
            build_web "$APP_NAME" "$VERSION" "$BUILD_NUMBER"
            ;;
        *)
            echo "❌ 错误: 不支持的平台 '$PLATFORM'"
            echo "支持的平台: android, apk, appbundle, ios, windows, macos, linux, web"
            exit 1
            ;;
    esac
}

# 构建 Android APK
build_android_apk() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    # 构建输出文件名
    local OUTPUT_NAME="${APP_NAME}_${VERSION}_${BUILD_NUMBER}.apk"
    
    echo "📦 构建 Android APK..."
    echo "目标输出文件名: $OUTPUT_NAME"
    
    # 执行构建
    puro flutter build apk --release --no-tree-shake-icons
    
    # 查找生成的APK文件
    local DEFAULT_APK="build/app/outputs/flutter-apk/app-release.apk"
    
    if [[ -n "$DEFAULT_APK" && -f "$DEFAULT_APK" ]]; then
        echo "✅ Flutter构建完成，找到默认输出文件: $(basename "$DEFAULT_APK")"
        
        # 重命名文件
        local OUTPUT_PATH="build/app/outputs/flutter-apk/$OUTPUT_NAME"
        cp "$DEFAULT_APK" "$OUTPUT_PATH"
        
        if [[ -f "$OUTPUT_PATH" ]]; then
            echo "✅ 文件已重命名为: $OUTPUT_NAME"
            echo "📁 文件位置: $OUTPUT_PATH"
            echo "📊 文件大小: $(du -h "$OUTPUT_PATH" | cut -f1)"
        else
            echo "❌ 文件重命名失败"
            echo "📁 原始文件位置: $DEFAULT_APK"
        fi
    else
        echo "❌ Flutter构建失败，未找到输出文件"
        echo "🔍 搜索其他可能的输出位置..."
        find build/ -name "*.apk" 2>/dev/null | head -5
        exit 1
    fi
}

# 构建 Android App Bundle
build_android_appbundle() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    # 构建输出文件名
    local OUTPUT_NAME="${APP_NAME}_${VERSION}_${BUILD_NUMBER}.aab"
    
    echo "📦 构建 Android App Bundle..."
    echo "目标输出文件名: $OUTPUT_NAME"
    
    # 执行构建
    puro flutter build appbundle --release --no-tree-shake-icons
    
    # 查找生成的AAB文件
    local DEFAULT_AAB="build/app/outputs/bundle/release/app-release.aab"
    
    if [[ -f "$DEFAULT_AAB" ]]; then
        echo "✅ Flutter构建完成，找到默认输出文件: $(basename "$DEFAULT_AAB")"
        
        # 重命名文件
        local OUTPUT_PATH="build/app/outputs/bundle/release/$OUTPUT_NAME"
        cp "$DEFAULT_AAB" "$OUTPUT_PATH"
        
        if [[ -f "$OUTPUT_PATH" ]]; then
            echo "✅ 文件已重命名为: $OUTPUT_NAME"
            echo "📁 文件位置: $OUTPUT_PATH"
            echo "📊 文件大小: $(du -h "$OUTPUT_PATH" | cut -f1)"
        else
            echo "❌ 文件重命名失败"
            echo "📁 原始文件位置: $DEFAULT_AAB"
        fi
    else
        echo "❌ Flutter构建失败，未找到输出文件"
        find build/ -name "*.aab" 2>/dev/null | head -5
        exit 1
    fi
}

# 构建 iOS
build_ios() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    echo "📱 构建 iOS..."
    
    # 执行构建
    puro flutter build ios --release --no-tree-shake-icons
    
    if [[ -d "build/ios/iphoneos" ]]; then
        echo "✅ iOS构建完成"
        echo "📁 输出目录: build/ios/iphoneos"
    else
        echo "❌ iOS构建失败"
        exit 1
    fi
}

# 构建 Windows
build_windows() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    # 构建输出文件名
    local OUTPUT_NAME="${APP_NAME}_${VERSION}_${BUILD_NUMBER}"
    
    echo "🪟 构建 Windows..."
    echo "目标输出目录: $OUTPUT_NAME"
    
    # 执行构建
    puro flutter build windows --release --no-tree-shake-icons
    
    if [[ -d "build/windows/x64/runner/Release" ]]; then
        echo "✅ Windows构建完成"
        echo "📁 输出目录: build/windows/x64/runner/Release"
        
        # 可以选择打包成zip
        local ZIP_NAME="${OUTPUT_NAME}.zip"
        if command -v zip &> /dev/null; then
            echo "📦 打包成zip..."
            (cd build/windows/x64/runner && zip -r "../../../$ZIP_NAME" Release)
            echo "✅ 压缩包: $ZIP_NAME"
            echo "📊 文件大小: $(du -h "$ZIP_NAME" 2>/dev/null | cut -f1 || echo "N/A")"
        fi
    else
        echo "❌ Windows构建失败"
        exit 1
    fi
}

# 构建 macOS
build_macos() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    echo "🍎 构建 macOS..."
    
    # 执行构建
    puro flutter build macos --release --no-tree-shake-icons
    
    if [[ -d "build/macos/Build/Products/Release" ]]; then
        echo "✅ macOS构建完成"
        echo "📁 输出目录: build/macos/Build/Products/Release"
    else
        echo "❌ macOS构建失败"
        exit 1
    fi
}

# 构建 Linux
build_linux() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    echo "🐧 构建 Linux..."
    
    # 执行构建
    puro flutter build linux --release --no-tree-shake-icons
    
    if [[ -d "build/linux/x64/release/bundle" ]]; then
        echo "✅ Linux构建完成"
        echo "📁 输出目录: build/linux/x64/release/bundle"
    else
        echo "❌ Linux构建失败"
        exit 1
    fi
}

# 构建 Web
build_web() {
    local APP_NAME="$1"
    local VERSION="$2"
    local BUILD_NUMBER="$3"
    
    # 构建输出文件名
    local OUTPUT_NAME="${APP_NAME}_${VERSION}_${BUILD_NUMBER}_web"
    
    echo "🌐 构建 Web..."
    
    # 执行构建
    puro flutter build web --release --no-tree-shake-icons
    
    if [[ -d "build/web" ]]; then
        echo "✅ Web构建完成"
        echo "📁 输出目录: build/web"
        
        # 可以选择打包成zip
        local ZIP_NAME="${OUTPUT_NAME}.zip"
        if command -v zip &> /dev/null; then
            echo "📦 打包成zip..."
            (cd build && zip -r "$ZIP_NAME" web)
            echo "✅ 压缩包: $ZIP_NAME"
            echo "📊 文件大小: $(du -h "build/$ZIP_NAME" 2>/dev/null | cut -f1 || echo "N/A")"
        fi
    else
        echo "❌ Web构建失败"
        exit 1
    fi
}

# 获取参数
ACTION="${1:-all}"
PLATFORM="${2:-android}"  # 默认平台为 android

case "$ACTION" in
    icon)
        check_flutter_commands
        puro flutter pub get
        generate_icons
        ;;

    splash)
        check_flutter_commands
        puro flutter pub get
        generate_splash
        ;;

    build_runner)
        check_flutter_commands
        puro flutter pub get
        run_build_runner
        ;;

    build)
        check_flutter_commands
        update_build_number
        replace_commit_hash_in_about
        puro flutter pub get
        run_flutter_build "$PLATFORM"
        restore_build_number
        restore_commit_hash_in_about
        ;;

    all|"")
        check_flutter_commands
        update_build_number
        replace_commit_hash_in_about
        puro flutter pub get
        generate_icons
        generate_splash
        run_build_runner
        run_flutter_build "$PLATFORM"
        restore_build_number
        restore_commit_hash_in_about
        echo "✅ 所有构建任务已完成！"
        ;;

    help|--help|-h)
        show_help
        ;;

    *)
        echo "❌ 错误: 未知参数 '$ACTION'"
        echo ""
        show_help
        exit 1
        ;;
esac