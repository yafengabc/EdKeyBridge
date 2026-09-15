# EdKeyBridge 构建脚本 (Windows)
# 作用：生成 comctl32 v6 清单资源 -> 编译为无控制台窗口的 GUI 程序 -> UPX 压缩。
# 用法：在 change_key 目录下右键“使用 PowerShell 运行”即可。
$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $dir

# rsrc 首次运行需联网下载并缓存；之后走本地模块缓存离线可用。
$env:GOSUMDB = "off"
Write-Host "[1/3] 生成 manifest + 图标资源 (rsrc) ..."
go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico icon/app.ico -arch amd64 -o rsrc_windows_amd64.syso

Write-Host "[2/3] 编译 EdKeyBridge.exe (无控制台窗口) ..."
$VERSION = ""
if (Get-Command git -ErrorAction SilentlyContinue) { $VERSION = (git describe --tags --always 2>$null) }
$LDFLAGS = "-s -w -H windowsgui"
if ($VERSION) { $LDFLAGS += " -X edkeybridge.version=$VERSION" }
go build -trimpath -ldflags=$LDFLAGS -o edkeybridge.exe .

Write-Host "[3/3] UPX 压缩 ..."
upx --best --lzma edkeybridge.exe

Write-Host "完成 -> $(Join-Path $dir 'edkeybridge.exe')"
