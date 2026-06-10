#!/bin/bash
set -e

# PFM 后端部署脚本
# 用法: bash scripts/deploy.sh

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SERVER_DIR="<服务目录: pfm>"
BINARY="$SERVER_DIR/pfm"
LOG_FILE="$SERVER_DIR/data/logs/log.log"

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}📦 开始部署 PFM 后端...${NC}"

# 1. 检查编译产物
if [ ! -f "$PROJECT_DIR/pfm" ]; then
    echo -e "${RED}❌ 未找到编译产物 $PROJECT_DIR/pfm，请先运行 bash dev.sh build${NC}"
    exit 1
fi

# 获取版本号
VERSION=$("$PROJECT_DIR/pfm" -v 2>/dev/null | grep -oP 'version: \K.*' || echo "unknown")
echo -e "📌 版本: ${GREEN}$VERSION${NC}"

# 2. 停止旧进程
echo -n "🛑 停止旧进程..."
PIDS=$(pgrep -f "$BINARY" 2>/dev/null || true)
if [ -n "$PIDS" ]; then
    echo "$PIDS" | xargs kill -9 2>/dev/null || true
    sleep 1
    # 确认进程已停止
    REMAINING=$(pgrep -f "$BINARY" 2>/dev/null || true)
    if [ -n "$REMAINING" ]; then
        echo -e "${RED} 进程未停止，强制终止...${NC}"
        echo "$REMAINING" | xargs kill -9 2>/dev/null || true
        sleep 1
    fi
    echo -e " ${GREEN}✓${NC}"
else
    echo -e " ${GREEN}(无运行进程)${NC}"
fi

# 3. 复制新二进制
echo -n "📋 复制二进制文件..."
cp "$PROJECT_DIR/pfm" "$BINARY"
echo -e " ${GREEN}✓${NC}"

# 4. 启动新进程
echo -n "🚀 启动新进程..."
cd "$SERVER_DIR"
nohup "$BINARY" -d data > /dev/null 2>&1 &
NEW_PID=$!
sleep 3

# 5. 验证启动
if kill -0 $NEW_PID 2>/dev/null; then
    echo -e " ${GREEN}✓${NC} (PID: $NEW_PID)"
    
    # 检查日志确认启动成功
    if grep -q "Starting server" "$LOG_FILE" 2>/dev/null; then
        PORT=$(grep "Starting server" "$LOG_FILE" | tail -1 | grep -oP ':\K[0-9]+')
        echo -e "${GREEN}✅ 部署成功！${NC}"
        echo -e "   版本: $VERSION"
        echo -e "   端口: $PORT"
        echo -e "   PID:  $NEW_PID"
    else
        echo -e "${YELLOW}⚠️  进程已启动，但未在日志中找到启动确认${NC}"
    fi
else
    echo -e "${RED}❌ 启动失败！${NC}"
    echo "最后 10 行日志:"
    tail -10 "$LOG_FILE"
    exit 1
fi
