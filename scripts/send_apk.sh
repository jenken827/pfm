#!/bin/bash
# 发送 APK 到 Telegram 的脚本
# 用法: ./send_apk.sh [apk_path]

set -e

# 读取 Telegram Bot Token
# 调用方提供 env 文件路径（内含 TELEGRAM_BOT_TOKEN=...）；不给默认值，避免把本机路径写进仓库
ENV_FILE="${TELEGRAM_ENV_FILE:?请设置 TELEGRAM_ENV_FILE 指向含 TELEGRAM_BOT_TOKEN 的 env 文件}"
if [ ! -f "$ENV_FILE" ]; then
    echo "❌ 找不到 env 文件: $ENV_FILE"
    exit 1
fi

# 从 .env 文件中提取 token
TOKEN=$(grep "^TELEGRAM_BOT_TOKEN=" "$ENV_FILE" | cut -d'=' -f2 | tr -d '"' | tr -d "'")
if [ -z "$TOKEN" ]; then
    echo "❌ 找不到 TELEGRAM_BOT_TOKEN"
    exit 1
fi

# 获取 chat_id（目标会话；由调用方通过环境变量给出，脚本不留默认值）
CHAT_ID="${TELEGRAM_CHAT_ID:?请设置 TELEGRAM_CHAT_ID（数字型目标会话 ID）}"

# 获取 APK 文件路径
if [ -n "$1" ]; then
    APK_PATH="$1"
else
    # 自动查找本仓库最新的 arm64 APK
    REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
    APK_PATH=$(ls -t "$REPO_DIR"/frontend/build/app/outputs/flutter-apk/*arm64*.apk 2>/dev/null | head -1)
fi

if [ -z "$APK_PATH" ] || [ ! -f "$APK_PATH" ]; then
    echo "❌ APK 文件不存在: $APK_PATH"
    exit 1
fi

# 获取文件名
FILENAME=$(basename "$APK_PATH")
FILESIZE=$(du -h "$APK_PATH" | cut -f1)

echo "📦 发送 APK: $FILENAME ($FILESIZE)"

# Bot API 服务地址（默认官方 API；自建 local Bot API server 用 TELEGRAM_BOT_API 覆盖）
BOT_API="${TELEGRAM_BOT_API:-https://api.telegram.org}"
RESPONSE=$(curl -s -X POST "${BOT_API}/bot${TOKEN}/sendDocument" \
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
