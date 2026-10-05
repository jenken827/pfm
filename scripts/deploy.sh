#!/bin/bash
set -euo pipefail

# PFM 后端部署脚本（部署已编译好的 $PROJECT_DIR/pfm）
# 用法: bash scripts/deploy.sh          （通常由 scripts/build_and_deploy.sh 调用）
#
# 流程: 校验编译产物 → mv 原子覆盖 → supervisorctl 重启 → 轮询健康检查
#
# 与 <服务目录: pfm>/deploy.sh 的区别: 本脚本**不编译、不提交 git**，只负责把已有的
# $PROJECT_DIR/pfm 部署上去；那个脚本是"提交 + 编译 + 部署"的完整流程。
#
# 两个关键点（都是踩过的坑）:
#
#   1. 用 mv 而不是 cp 覆盖运行中的二进制。
#      cp 覆盖正在执行的文件会报 "Text file busy"(ETXTBSY)；rename(2) 只替换目录项，
#      运行中的进程继续持有旧 inode，因此无需先停服务。
#      前提: 项目目录与部署目录在同一文件系统（下面会检查）。
#      注意: mv 会把 $PROJECT_DIR/pfm 移走（消费掉），下次部署前需重新编译。
#
#   2. 用 supervisorctl 而不是 kill -9 + nohup。
#      pfm 由 supervisord 托管（autorestart=true），kill 后会被立刻拉起，与 nohup
#      起的新进程抢端口；而且 `pfm -d data` 缺少 server 子命令——根命令只打印帮助
#      后退出（cmd/root.go），根本不会启动服务。

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="<服务实例目录>"
BINARY="$DEPLOY_DIR/pfm"
HEALTH_URL="http://localhost:10999/"

# supervisorctl / supervisord 配置装在 <本机工具目录> 下，不在 PATH 里
SUPERVISOR_CONF="<supervisord 配置>"
SUPERVISORCTL="supervisorctl"
PROGRAM="pfm"

# ---------------------------------------------------------------- 前置检查
[ -f "$PROJECT_DIR/pfm" ] || {
    echo "❌ 未找到编译产物 $PROJECT_DIR/pfm，请先运行 bash dev.sh build" >&2
    exit 1
}
[ -x "$SUPERVISORCTL" ] || { echo "❌ 未找到 supervisorctl: $SUPERVISORCTL" >&2; exit 1; }
[ -f "$SUPERVISOR_CONF" ] || { echo "❌ 未找到 supervisord 配置: $SUPERVISOR_CONF" >&2; exit 1; }
[ "$(stat -c %d "$PROJECT_DIR")" = "$(stat -c %d "$DEPLOY_DIR")" ] || {
    echo "❌ $PROJECT_DIR 与 $DEPLOY_DIR 不在同一文件系统，无法用 mv 原子覆盖运行中的二进制" >&2
    exit 1
}

echo "📌 待部署版本: $("$PROJECT_DIR/pfm" version)"

# ---------------------------------------------------------------- 1. mv 原子覆盖
echo ""
echo "📦 Step 1: mv 原子覆盖..."
mv -f "$PROJECT_DIR/pfm" "$BINARY"
echo "  ✅ 已覆盖: $BINARY"

# ---------------------------------------------------------------- 2. 重启
echo ""
echo "🚀 Step 2: supervisorctl 重启..."
"$SUPERVISORCTL" -c "$SUPERVISOR_CONF" restart "$PROGRAM"
echo "  ✅ 已重启"

# ---------------------------------------------------------------- 3. 健康检查
echo ""
echo "⏳ Step 3: 等待服务就绪..."
# supervisord startsecs=5，且启动要跑 bootstrap/DB/resumeDispatch，故轮询而非固定 sleep
for i in $(seq 1 60); do
    code=$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL" || true)
    if [ -n "$code" ] && [ "$code" != "000" ]; then
        echo "  ✅ 服务已就绪 (HTTP $code, 用时 ${i}s)"
        echo ""
        echo "🎉 部署完成！"
        exit 0
    fi
    sleep 1
done

echo "  ❌ 60s 内服务未就绪，请检查日志:" >&2
echo "     $DEPLOY_DIR/data/logs/supervisor.log" >&2
"$SUPERVISORCTL" -c "$SUPERVISOR_CONF" status "$PROGRAM" || true
exit 1
