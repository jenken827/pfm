#!/usr/bin/env bash
# 构建指定平台的发布包（可执行文件 + 示例配置 + 许可证），并打印 sha256。
#
# 用法：
#   bash scripts/package.sh --version 0.1.0 --goos linux
#   bash scripts/package.sh --version 0.1.0 --goos windows --goarch amd64
#
# 产物：<out>/pfm_<版本>_<YYYYMMDD>-<短commit8>_<goos>-<goarch>.tar.gz  （windows 为 .zip）
#
# SQLite 是纯 Go 实现，代码里没有 cgo，所以任何平台都能交叉打其它平台的包。
set -euo pipefail

version=""
goos=""
goarch="amd64"
out="dist"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="$2"; shift 2 ;;
    --goos) goos="$2"; shift 2 ;;
    --goarch) goarch="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done
[[ -n "$version" && -n "$goos" ]] || {
  echo "用法: $0 --version <版本> --goos <linux|windows> [--goarch amd64] [--out <目录>]" >&2
  exit 1
}

cd "$(dirname "$0")/.."

short=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
  short="${short}-dirty"
fi
platform="${goos}-${goarch}"
base="pfm_${version}_$(date +%Y%m%d)-${short}_${platform}"

bin="pfm"
[[ "$goos" == "windows" ]] && bin="pfm.exe"
mkdir -p "$out"
rm -f "$out/${base}.tar.gz" "$out/${base}.zip"

echo "🔨 构建 ${platform}（版本 ${version}）"
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
  -trimpath \
  -ldflags "-w -s -X github.com/jenken827/pfm/globals.Version=${version}" \
  -o "$out/${base}/${bin}" .

# 包里只放运行需要的三样：可执行文件、示例配置、许可证
cp config.example.yaml LICENSE "$out/${base}/"

case "$goos" in
  windows)
    python3 - "$out/${base}" "$out/${base}.zip" <<'PY'
import sys, zipfile, pathlib
src, dst = pathlib.Path(sys.argv[1]), sys.argv[2]
with zipfile.ZipFile(dst, 'w', zipfile.ZIP_DEFLATED) as z:
    for f in sorted(src.iterdir()):
        info = zipfile.ZipInfo(f.name, date_time=(2026, 1, 1, 0, 0, 0))
        info.compress_type = zipfile.ZIP_DEFLATED
        info.external_attr = 0o644 << 16
        z.writestr(info, f.read_bytes())
PY
    ;;
  *)
    tar -czf "$out/${base}.tar.gz" -C "$out/${base}" --owner=0 --group=0 --numeric-owner .
    ;;
esac

rm -rf "$out/${base}"

pkg="$out/${base}.tar.gz"
[[ "$goos" == "windows" ]] && pkg="$out/${base}.zip"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$pkg"
else
  shasum -a 256 "$pkg"
fi
