#!/usr/bin/env bash
# 构建 BLAKE3 原生库（Dart FFI 加速，设计见 DESIGN.md §14.12.4）。
#
# 用法：
#   bash native/build_native.sh                 # 宿主平台（本地 dev / 单测用）
#   bash native/build_native.sh android         # Android arm64-v8a（发布用的 ABI）
#   bash native/build_native.sh android-multi   # Android 四个 ABI 都编
#   bash native/build_native.sh clean           # 清掉构建产物
#
# 约定：
# * 不写死任何本机路径：NDK 从 ANDROID_NDK_HOME / ANDROID_NDK_ROOT /
#   $ANDROID_SDK_ROOT/ndk 推导，取版本号最大的一个。
# * 产物只落在 .gitignore 覆盖的目录里（target/、android jniLibs/、build/native/），
#   **不要**把 .so 提交进仓库。
# * 失败就非零退出：调用方（flutter build android）只把它当可选加速，
#   缺 cargo / NDK 时 App 会退回纯 Dart 哈希，功能不受影响。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CRATE_DIR="$SCRIPT_DIR/blake3_ffi"
ACTION="${1:-host}"

info() { echo "🔧 $*"; }
die() {
    echo "❌ $*" >&2
    exit 1
}

require_cargo() {
    command -v cargo >/dev/null 2>&1 ||
        die "找不到 cargo：需要 Rust 工具链（https://rustup.rs）。没有也能构建 App，只是不走原生加速。"
}

host_tag() {
    case "$(uname -s)-$(uname -m)" in
        Linux-x86_64) echo "linux-x64" ;;
        Linux-aarch64 | Linux-arm64) echo "linux-arm64" ;;
        Darwin-arm64) echo "macos-arm64" ;;
        Darwin-x86_64) echo "macos-x64" ;;
        MINGW* | MSYS*) echo "windows-x64" ;;
        *) echo "$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)" ;;
    esac
}

build_host() {
    require_cargo
    info "构建宿主原生库（$(host_tag)）..."
    (
        cd "$CRATE_DIR"
        cargo build --release
    )
    local out="$FRONTEND_DIR/build/native/$(host_tag)"
    mkdir -p "$out"
    local lib
    case "$(uname -s)" in
        Darwin) lib="libpfm_blake3.dylib" ;;
        MINGW* | MSYS*) lib="pfm_blake3.dll" ;;
        *) lib="libpfm_blake3.so" ;;
    esac
    cp "$CRATE_DIR/target/release/$lib" "$out/"
    echo "✅ 宿主原生库：$out/$lib"
    echo "   自测（会走原生后端）：PFM_BLAKE3_LIB=$out/$lib bash dev.sh flutter test"
}

# 取 NDK 根目录：显式环境变量优先，其次 SDK 下按版本号取最新
detect_ndk() {
    local candidate
    for candidate in "${ANDROID_NDK_HOME:-}" "${ANDROID_NDK_ROOT:-}"; do
        if [ -n "$candidate" ] && [ -d "$candidate" ]; then
            echo "$candidate"
            return
        fi
    done
    local sdk="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
    [ -n "$sdk" ] || die "找不到 Android NDK：请设置 ANDROID_NDK_HOME 或 ANDROID_SDK_ROOT"
    [ -d "$sdk/ndk" ] || die "找不到 NDK 目录：$sdk/ndk"
    ls -1 "$sdk/ndk" | sort -V | tail -1 | sed "s|^|$sdk/ndk/|"
}

target_triple_for() {
    case "$1" in
        arm64-v8a) echo "aarch64-linux-android" ;;
        armeabi-v7a) echo "armv7-linux-androideabi" ;;
        x86_64) echo "x86_64-linux-android" ;;
        x86) echo "i686-linux-android" ;;
        *) die "未知 ABI：$1（支持 arm64-v8a / armeabi-v7a / x86_64 / x86）" ;;
    esac
}

clang_for() {
    case "$1" in
        arm64-v8a) echo "aarch64-linux-android21-clang" ;;
        armeabi-v7a) echo "armv7a-linux-androideabi21-clang" ;;
        x86_64) echo "x86_64-linux-android21-clang" ;;
        x86) echo "i686-linux-android21-clang" ;;
        *) die "未知 ABI：$1" ;;
    esac
}

build_android() {
    require_cargo
    local abis="$1"
    local ndk
    ndk="$(detect_ndk)"
    local host_dir
    case "$(uname -s)" in
        Darwin) host_dir="darwin-x86_64" ;;
        *) host_dir="linux-x86_64" ;;
    esac
    local toolchain="$ndk/toolchains/llvm/prebuilt/$host_dir/bin"
    [ -d "$toolchain" ] || die "找不到 NDK 工具链：$toolchain"

    local jni_dir="$FRONTEND_DIR/android/app/src/main/jniLibs"
    local abi triple clang linker_var
    for abi in $abis; do
        triple="$(target_triple_for "$abi")"
        clang="$toolchain/$(clang_for "$abi")"
        [ -x "$clang" ] || die "找不到 clang：$clang"
        if ! rustup target list --installed 2>/dev/null | grep -qx "$triple"; then
            die "缺少 Rust target：rustup target add $triple"
        fi
        linker_var="CARGO_TARGET_$(echo "$triple" | tr 'a-z-' 'A-Z_')_LINKER"
        info "交叉编译 $abi（$triple）..."
        # blake3 会用 cc 编 C/汇编内核，所以不只 LINKER，CC/AR 也要指到 NDK；
        # NDK 的 clang wrapper 名字里已带 target triple，直接当 CC 用即可。
        (
            cd "$CRATE_DIR"
            env \
                "$linker_var=$clang" \
                "CC_$triple=$clang" \
                "AR_$triple=$toolchain/llvm-ar" \
                "RANLIB_$triple=$toolchain/llvm-ranlib" \
                cargo build --release --target "$triple"
        )
        mkdir -p "$jni_dir/$abi"
        cp "$CRATE_DIR/target/$triple/release/libpfm_blake3.so" "$jni_dir/$abi/"
        # 去掉调试符号：.so 从 ~630 KB 降到 ~440 KB（动态符号表保留，dlsym 照常）
        local strip="$toolchain/llvm-strip"
        if [ -x "$strip" ]; then
            "$strip" --strip-unneeded "$jni_dir/$abi/libpfm_blake3.so"
        fi
        echo "✅ $abi → $jni_dir/$abi/libpfm_blake3.so"
    done
}

clean_build() {
    info "清理原生构建产物..."
    rm -rf "$CRATE_DIR/target" "$FRONTEND_DIR/build/native" \
        "$FRONTEND_DIR/android/app/src/main/jniLibs"
    echo "✅ 已清理"
}

case "$ACTION" in
    host) build_host ;;
    android) build_android "${2:-arm64-v8a}" ;;
    android-multi) build_android "${2:-arm64-v8a armeabi-v7a x86_64}" ;;
    clean) clean_build ;;
    -h | --help | help)
        sed -n '2,12p' "$0"
        ;;
    *)
        die "未知动作：$ACTION（支持 host / android / android-multi / clean）"
        ;;
esac
