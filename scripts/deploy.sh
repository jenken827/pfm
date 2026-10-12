#!/bin/bash
set -euo pipefail

# PFM 发布辅助脚本：把已编译好的 $PROJECT_DIR/pfm 发布到目标目录，执行重启命令，再做健康检查。
#
# 用法: bash scripts/deploy.sh
#   （通常由 `bash dev.sh deploy` 调用。**可选**：不用它、自己写一套发布流程也完全可以。）
#
# 环境变量（仓库内不留任何环境假设，全部由调用方给出）:
#   PFM_DEPLOY_DIR    必填，目标目录；二进制发布为 $PFM_DEPLOY_DIR/pfm
#   PFM_RESTART_CMD   必填，重启服务的整条命令（如 `supervisorctl restart pfm`、
#                     `systemctl restart pfm`、`docker compose restart pfm`）
#   PFM_HEALTH_URL    选填，健康检查地址，默认 http://localhost:10999/
#
# 两个与"用什么进程管理器"无关的通用要点:
#
#   1. 用 mv 覆盖运行中的二进制，而不是 cp。
#      cp 以写方式打开正在执行的 inode → "Text file busy"(ETXTBSY)；
#      rename(2) 只替换目录项，运行中的进程继续持有旧 inode，因此无需先停服务。
#      前提：源目录与目标目录在同一文件系统（下面会校验）。
#      注意：mv 会把 $PROJECT_DIR/pfm 移走（消费掉），下次发布前需重新编译。
#
#   2. 重启必须交给**部署方自己的**进程管理器。
#      本项目代码不做守护/自拉起；如果进程由某个管理器托管（且配了自动重启），
#      绕过管理器手工起进程只会和管理器抢端口。具体用哪一条命令由 PFM_RESTART_CMD 给出。

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="${PFM_DEPLOY_DIR:?请设置 PFM_DEPLOY_DIR 指向目标目录}"
RESTART_CMD="${PFM_RESTART_CMD:?请设置 PFM_RESTART_CMD 为重启服务的整条命令}"
HEALTH_URL="${PFM_HEALTH_URL:-http://localhost:10999/}"
BINARY="$DEPLOY_DIR/pfm"

# ---------------------------------------------------------------- 前置检查
[ -f "$PROJECT_DIR/pfm" ] || {
    echo "❌ 未找到编译产物 $PROJECT_DIR/pfm，请先运行 bash dev.sh build" >&2
    exit 1
}
[ -d "$DEPLOY_DIR" ] || {
    echo "❌ 目标目录不存在: $DEPLOY_DIR（PFM_DEPLOY_DIR）" >&2
    exit 1
}
[ "$(stat -c %d "$PROJECT_DIR")" = "$(stat -c %d "$DEPLOY_DIR")" ] || {
    echo "❌ $PROJECT_DIR 与 $DEPLOY_DIR 不在同一文件系统，无法用 mv 原子覆盖运行中的二进制" >&2
    exit 1
}

echo "📌 待发布版本: $("$PROJECT_DIR/pfm" version)"

# ---------------------------------------------------------------- 1. mv 原子覆盖
echo ""
echo "📦 Step 1: mv 原子覆盖..."
mv -f "$PROJECT_DIR/pfm" "$BINARY"
echo "  ✅ 已覆盖: $BINARY"

# ---------------------------------------------------------------- 2. 重启
echo ""
echo "🚀 Step 2: 重启服务（$RESTART_CMD）..."
sh -c "$RESTART_CMD"
echo "  ✅ 已执行重启命令"

# ---------------------------------------------------------------- 3. 健康检查
echo ""
echo "⏳ Step 3: 等待服务就绪..."
# 不固定 sleep：启动要跑 bootstrap/DB/resumeDispatch，故轮询
for i in $(seq 1 60); do
    code=$(curl -s -o /dev/null -w '%{http_code}' "$HEALTH_URL" || true)
    if [ -n "$code" ] && [ "$code" != "000" ]; then
        echo "  ✅ 服务已就绪 (HTTP $code, 用时 ${i}s)"
        echo ""
        echo "🎉 发布完成！"
        exit 0
    fi
    sleep 1
done

echo "  ❌ 60s 内服务未就绪，请检查日志:" >&2
echo "     日志目录: $DEPLOY_DIR/data/logs/" >&2
exit 1
