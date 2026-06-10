#!/bin/bash
# 发送 APK 到 Telegram 的脚本
# 用法: ./send_apk.sh [apk_path]

set -e

# 读取 Telegram Bot Token
ENV_FILE="<env 文件>"
if [ ! -f "$ENV_FILE" ]; then
    echo "❌ 找不到 .env 文件: $ENV_FILE"
    exit 1
fi

# 从 .env 文件中提取 token
TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'")
if [ -z "$TOKEN" ]; then
    echo "❌ 找不到 TELEGRAM_BOT_TOKEN"
    exit 1
fi

# 获取 chat_id
CHAT_ID="${TELEGRAM_ALLOWED_USERS:-}"

# 获取 APK 文件路径
if [ -n "$1" ]; then
    APK_PATH="$1"
else
    # 自动查找最新的 arm64 APK
    APK_PATH=$(ls -t <repo: pfm>/frontend/build/app/outputs/flutter-apk/*arm64*.apk 2>/dev/null | head -1)
fi

if [ -z "$APK_PATH" ] || [ ! -f "$APK_PATH" ]; then
    echo "❌ APK 文件不存在: $APK_PATH"
    exit 1
fi

# 获取文件名
FILENAME=$(basename "$APK_PATH")
FILESIZE=$(du -h "$APK_PATH" | cut -f1)

echo "📦 发送 APK: $FILENAME ($FILESIZE)"

# 使用本地 Bot API 服务器（无 50MB 限制）
RESPONSE=$(curl -s -X POST "http://127.0.0.1:12311/bot${TOKEN}/sendDocument" \
    -F "chat_id=${CHAT_ID}" \
    -F "document=@${APK_PATH}" \
    -F "caption=${FILENAME}")

# 检查结果
if echo "$RESPONSE" | grep -q '"ok":true'; then
    echo "✅ 发送完成"
else
    DESC=$(echo "$RESPONSE" | grep -o '"description":"[^"]*"' | cut -d'"' -f4)
    echo "❌ 发送失败: $DESC"
    exit 1
fi
