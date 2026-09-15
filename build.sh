#!/usr/bin/env bash
# EdKeyBridge build script (bash, works on Git Bash / CI / Linux)
# Generates the comctl32 v6 manifest + icon resource, compiles a console-less
# GUI executable, then UPX-compresses it. Run from the project root.
set -euo pipefail

cd "$(dirname "$0")"
export GOSUMDB=off

echo "[1/3] rsrc: manifest + icon -> src/rsrc_windows_amd64.syso"
# 必须输出到 src/：Go 只链接**包目录**下的 .syso，放根目录不会被打进二进制。
go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico icon/app.ico -arch amd64 -o src/rsrc_windows_amd64.syso

echo "[2/3] go build (windowsgui, version stamped)"
VERSION="$(git describe --tags --always 2>/dev/null || true)"
LDFLAGS="-s -w -H windowsgui"
if [ -n "$VERSION" ]; then
  # 必须写 main.version：实测 -X 对 main 包**不认 import path**
  # （-X edkeybridge/src.version 与 -X edkeybridge.version 都无效，会静默保持 dev）
  LDFLAGS="$LDFLAGS -X main.version=$VERSION"
fi
go build -trimpath -ldflags="$LDFLAGS" -o edkeybridge.exe ./src

echo "[3/3] upx --best --lzma"
upx --best --lzma edkeybridge.exe

echo "done -> $(pwd)/edkeybridge.exe"
