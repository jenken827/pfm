#!/bin/bash

# Build & Dev script for Personal File Manager (Go Server)
# 所有常用命令的统一入口，避免重复执行错误命令

set -e

# Get current directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# ============================================================
# 帮助信息
# ============================================================

show_help() {
    cat << EOF
用法: $0 [命令] [选项]

命令:
  build          编译 Go 服务器（自动注入版本号）
  run            编译并运行服务器
  deploy         发布到目标目录（编译 + 调用 scripts/deploy.sh；需 PFM_* 环境变量）
  proto          生成 protobuf 代码（Go + Dart）
  proto-go       仅生成 Go protobuf 代码
  proto-dart     仅生成 Dart protobuf 代码
  test           运行所有测试（Go）
  test-web       运行 Web UI 测试（node:test，假 DOM，无需浏览器）
  test-cover     运行测试并生成覆盖率报告
  tidy           执行 go mod tidy
  lint           运行 golangci-lint 代码检查
  clean          清理构建产物
  flutter        执行 Flutter 开发任务（见 flutter help）
  native         构建 BLAKE3 原生库（可选加速；host / android / android-multi / clean）
  flutter-clean  清理 Flutter 构建缓存
  help           显示此帮助信息

选项:
  build/run 命令:
    -o <path>        指定输出文件路径（默认: ./pfm）
    --skip-tests     跳过测试缓存（-count=1）
    
  test/test-cover 命令:
    -v               详细输出
    -run <pattern>   运行匹配的测试
    -short           运行短测试
    -count <n>       运行 n 次（禁用缓存）
    
  lint 命令:
    --fix            自动修复问题

示例:
  $0 build                        # 编译服务器
  $0 build -o /usr/local/bin/pfm  # 编译到指定路径
  $0 build --skip-tests           # 跳过测试缓存
  $0 run                          # 编译并运行
  $0 deploy                       # 发布到目标目录（需 PFM_* 环境变量，见 README §3）
  $0 dev                          # 开发模式运行（不编译）
  $0 proto                        # 生成所有 protobuf 代码
  $0 proto-go                     # 仅生成 Go protobuf 代码
  $0 proto-dart                   # 仅生成 Dart protobuf 代码
  $0 test                         # 运行测试（Go）
  $0 test-web                     # 运行 Web UI 测试（node:test）
  $0 test -v                      # 详细输出
  $0 test -run TestInitUpload     # 运行特定测试
  $0 test-cover                   # 运行测试并生成覆盖率报告
  $0 tidy                         # 清理 Go 模块依赖
  $0 lint                         # 代码检查
  $0 lint --fix                   # 自动修复
  $0 clean                        # 清理构建产物
  $0 flutter-clean                # 清理 Flutter 构建缓存

注意:
  - build 和 run 命令会自动从 git commit hash 和日期生成版本号
  - proto 命令需要在 proto/ 目录下执行 buf generate
  - 项目模块路径: jenken827.com/jenken827/pfm
EOF
}

# ============================================================
# 通用辅助
# ============================================================

# 获取版本号：日期 + commit hash
get_version() {
    local COMMIT_HASH=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
    local DATETIME=$(date +"%Y%m%d%H%M%S")
    echo "${DATETIME}-${COMMIT_HASH}"
}

# 检查命令是否存在
check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo "❌ 错误: 命令 '$1' 未找到，请安装后重试"
        exit 1
    fi
}

# ============================================================
# 命令实现
# ============================================================

# 编译 Go 服务器
cmd_build() {
    local OUTPUT="./pfm"
    local SKIP_TESTS=false
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -o|--output)
                OUTPUT="$2"
                shift 2
                ;;
            --skip-tests)
                SKIP_TESTS=true
                shift
                ;;
            -*)
                echo "❌ 未知选项: $1"
                exit 1
                ;;
            *)
                OUTPUT="$1"
                shift
                ;;
        esac
    done
    
    local VERSION=$(get_version)

    echo "🔨 编译 Go 服务器..."
    echo "📌 版本: $VERSION"
    echo "📌 输出: $OUTPUT"

    check_command "go"
    
    local BUILD_ARGS="-ldflags \"-w -s -X jenken827.com/jenken827/pfm/globals.Version=${VERSION}\""
    if [ "$SKIP_TESTS" = true ]; then
        echo "📌 跳过测试缓存（注意：go build 不支持 -count 参数）"
    fi
    
    eval go build $BUILD_ARGS -o \"$OUTPUT\" .

    echo "✅ 编译完成！版本: $VERSION"
}

# 编译并运行
cmd_run() {
    local OUTPUT="./pfm"
    local SKIP_TESTS=false
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -o|--output)
                OUTPUT="$2"
                shift 2
                ;;
            --skip-tests)
                SKIP_TESTS=true
                shift
                ;;
            -*)
                echo "❌ 未知选项: $1"
                exit 1
                ;;
            *)
                OUTPUT="$1"
                shift
                ;;
        esac
    done
    
    local VERSION=$(get_version)

    echo "🔨 编译并运行 Go 服务器..."
    echo "📌 版本: $VERSION"

    check_command "go"
    
    local BUILD_ARGS="-ldflags \"-w -s -X jenken827.com/jenken827/pfm/globals.Version=${VERSION}\""
    if [ "$SKIP_TESTS" = true ]; then
        echo "📌 跳过测试缓存（注意：go build 不支持 -count 参数）"
    fi
    
    eval go build $BUILD_ARGS -o \"$OUTPUT\" .
    echo "🚀 启动服务器..."
    "$OUTPUT" server -d ./data
}

# 发布：编译后调用 scripts/deploy.sh，把产物发布到目标目录并重启服务
#
# 只做"编译 + 发布"，不含 git 提交/推送。
# 目标目录 / 重启命令 / 健康检查地址由 PFM_* 环境变量传入（见 README §3 与 scripts/deploy.sh）。
# **可选**：不用这个脚本、自己写一套发布流程也完全可以。
cmd_deploy() {
    echo "🚀 发布 PFM..."
    cmd_build
    echo ""
    bash "$SCRIPT_DIR/scripts/deploy.sh"
}

# 开发模式运行（不编译，直接 go run）
cmd_dev() {
    echo "🚀 开发模式运行..."
    check_command "go"
    go run . server -d ./data
}

# 生成所有 protobuf 代码
cmd_proto() {
    echo "📦 生成所有 protobuf 代码..."
    check_command "buf"
    cmd_proto_go
    cmd_proto_dart
    echo "✅ 所有 protobuf 代码生成完成！"
}

# 仅生成 Go protobuf 代码
cmd_proto_go() {
    echo "📦 生成 Go protobuf 代码..."
    check_command "buf"
    check_command "protoc-gen-go"
    check_command "protoc-gen-sgorm"

    cd "$SCRIPT_DIR/proto"
    buf generate
    cd "$SCRIPT_DIR"

    echo "✅ Go protobuf 代码生成完成！"
}

# 仅生成 Dart protobuf 代码
cmd_proto_dart() {
    echo "📦 生成 Dart protobuf 代码..."
    check_command "buf"
    # 跳过 protoc-gen-dart 检测（在 Windows 上会卡住）

    cd "$SCRIPT_DIR/proto"
    buf generate --template buf.gen.dart.yaml
    cd "$SCRIPT_DIR"

    echo "✅ Dart protobuf 代码生成完成！"
}

# 运行测试
cmd_test() {
    local -a TEST_ARGS=()
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -v)
                TEST_ARGS+=("-v")
                shift
                ;;
            -run)
                TEST_ARGS+=("-run" "$2")
                shift 2
                ;;
            -short)
                TEST_ARGS+=("-short")
                shift
                ;;
            -count)
                TEST_ARGS+=("-count" "$2")
                shift 2
                ;;
            -*)
                echo "❌ 未知选项: $1"
                exit 1
                ;;
            *)
                TEST_ARGS+=("$1")
                shift
                ;;
        esac
    done
    
    echo "🧪 运行测试..."
    check_command "go"
    # 用数组展开，不要再 eval：eval 会二次解析，-run 里的 | ( ) 等正则元字符会被当成 shell 语法
    go test "${TEST_ARGS[@]}" ./...
    echo "✅ 测试完成！"
}

# 运行 Web UI 测试（server/webui/test/）
#
# 用 node:test + 自带的假 DOM（test/support/），不装 npm 依赖、不启浏览器：
# 视图模块在假 document / 假 fetch 上渲染，断言页面上用户看得见的文案。
cmd_test_web() {
    echo "🧪 运行 Web UI 测试..."
    check_command "node"
    node --test "$@" "${SCRIPT_DIR}"/server/webui/test/*.test.mjs
    echo "✅ Web UI 测试完成！"
}

# 运行测试并生成覆盖率报告
cmd_test_cover() {
    local -a TEST_ARGS=()
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -v)
                TEST_ARGS+=("-v")
                shift
                ;;
            -run)
                TEST_ARGS+=("-run" "$2")
                shift 2
                ;;
            -short)
                TEST_ARGS+=("-short")
                shift
                ;;
            -count)
                TEST_ARGS+=("-count" "$2")
                shift 2
                ;;
            -*)
                echo "❌ 未知选项: $1"
                exit 1
                ;;
            *)
                TEST_ARGS+=("$1")
                shift
                ;;
        esac
    done
    
    echo "🧪 运行测试并生成覆盖率报告..."
    check_command "go"

    go test -coverprofile=coverage.out "${TEST_ARGS[@]}" ./...
    go tool cover -func=coverage.out

    echo ""
    echo "📊 详细覆盖率报告: go tool cover -html=coverage.out"
    echo "✅ 测试完成！"
}

# go mod tidy
cmd_tidy() {
    echo "🧹 清理 Go 模块依赖..."
    check_command "go"
    go mod tidy
    echo "✅ 模块依赖清理完成！"
}

# 代码检查
cmd_lint() {
    local -a LINT_ARGS=()
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --fix)
                LINT_ARGS+=("--fix")
                shift
                ;;
            -*)
                echo "❌ 未知选项: $1"
                exit 1
                ;;
            *)
                LINT_ARGS+=("$1")
                shift
                ;;
        esac
    done
    
    echo "🔍 运行代码检查..."
    check_command "golangci-lint"
    golangci-lint run "${LINT_ARGS[@]}" ./...
    echo "✅ 代码检查完成！"
}

# 清理构建产物
cmd_clean() {
    echo "🧹 清理构建产物..."
    rm -f ./pfm
    rm -f ./coverage.out
    echo "✅ 清理完成！"
}

# Flutter 清理构建缓存
cmd_flutter_clean() {
    echo "🧹 清理 Flutter 构建缓存..."
    check_command "puro"
    cd "$SCRIPT_DIR/frontend"
    puro flutter clean
    echo "✅ Flutter 缓存清理完成！"
}

# 构建 BLAKE3 原生库（Dart FFI 加速，见 DESIGN.md §14.12.4）。
# 可选步骤：缺 Rust / NDK 时只影响哈希吞吐，App 会退回纯 Dart。
cmd_native() {
    cd "$SCRIPT_DIR/frontend"
    if [[ ! -f "native/build_native.sh" ]]; then
        echo "❌ 错误: frontend/native/build_native.sh 不存在"
        exit 1
    fi
    bash native/build_native.sh "$@"
}

# Flutter 开发任务
cmd_flutter() {
    cd "$SCRIPT_DIR/frontend"
    if [[ ! -f "dev.sh" ]]; then
        echo "❌ 错误: frontend/dev.sh 不存在"
        exit 1
    fi

    if [[ "$1" == "help" || "$1" == "--help" || "$1" == "-h" ]]; then
        bash dev.sh help
    else
        bash dev.sh "$@"
    fi
}

# ============================================================
# 主入口
# ============================================================

ACTION="${1:-help}"
shift 2>/dev/null || true
FLUTTER_ARGS="$@"

case "$ACTION" in
    build)
        cmd_build "$@"
        ;;
    run)
        cmd_run "$@"
        ;;
    deploy)
        cmd_deploy
        ;;
    dev)
        cmd_dev
        ;;
    proto)
        cmd_proto
        ;;
    proto-go)
        cmd_proto_go
        ;;
    proto-dart)
        cmd_proto_dart
        ;;
    test)
        cmd_test "$@"
        ;;
    test-web)
        cmd_test_web "$@"
        ;;
    test-cover)
        cmd_test_cover "$@"
        ;;
    tidy)
        cmd_tidy
        ;;
    lint)
        cmd_lint "$@"
        ;;
    clean)
        cmd_clean
        ;;
    flutter-clean)
        cmd_flutter_clean
        ;;
    flutter)
        cmd_flutter $FLUTTER_ARGS
        ;;
    native)
        cmd_native "$@"
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo "❌ 错误: 未知命令 '$ACTION'"
        echo ""
        show_help
        exit 1
        ;;
esac
