#!/bin/bash
set -e

# PFM 一键构建 + 发布脚本
# 用法: bash scripts/build_and_deploy.sh [--apk]
#
# 发布步骤（scripts/deploy.sh）需要 PFM_DEPLOY_DIR / PFM_RESTART_CMD 环境变量；
# 发 APK（--apk）需要 TELEGRAM_* 环境变量。两者都见对应脚本的头部注释。

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_APK=false

# 解析参数
for arg in "$@"; do
    case $arg in
        --apk) BUILD_APK=true ;;
    esac
done

echo "🔨 构建后端..."
cd "$PROJECT_DIR"
bash dev.sh build

if [ "$BUILD_APK" = true ]; then
    echo ""
    echo "📱 构建前端 APK..."
    cd "$PROJECT_DIR/frontend"
    bash dev.sh build apk
fi

echo ""
echo "🚀 部署后端..."
bash "$PROJECT_DIR/scripts/deploy.sh"

if [ "$BUILD_APK" = true ]; then
    echo ""
    echo "📤 发送 APK..."
    bash "$PROJECT_DIR/scripts/send_apk.sh"
fi
