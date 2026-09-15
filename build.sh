#!/usr/bin/env bash
# EdKeyBridge build script (bash, works on Git Bash / CI / Linux)
# Generates the comctl32 v6 manifest + icon resource, compiles a console-less
# GUI executable, then UPX-compresses it. Run from the project root.
set -euo pipefail

cd "$(dirname "$0")"
export GOSUMDB=off

echo "[1/3] rsrc: manifest + icon -> rsrc_windows_amd64.syso"
go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico icon/app.ico -arch amd64 -o rsrc_windows_amd64.syso

echo "[2/3] go build (windowsgui, version stamped)"
VERSION="$(git describe --tags --always 2>/dev/null || true)"
LDFLAGS="-s -w -H windowsgui"
if [ -n "$VERSION" ]; then
  LDFLAGS="$LDFLAGS -X edkeybridge.version=$VERSION"
fi
go build -trimpath -ldflags="$LDFLAGS" -o edkeybridge.exe .

echo "[3/3] upx --best --lzma"
upx --best --lzma edkeybridge.exe

echo "done -> $(pwd)/edkeybridge.exe"
