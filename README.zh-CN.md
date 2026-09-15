# EdKeyBridge

把 **RustDesk 安卓客户端** 发往 **Windows** 被控端的键盘输入，转换成
《精英危险》(Elite Dangerous) 能真正响应的**真实物理按键**。

[English](README.md) · [更新日志](CHANGELOG.zh-CN.md) · [Changelog](CHANGELOG.md)

## 为什么需要它

RustDesk 的安卓客户端不编译桌面端的键盘管线。它会以 Unicode 文本序列的形式把按键
送到 Windows 被控端，被控端用 `SendInput(KEYEVENTF_UNICODE)` 注入——这只产生
`WM_CHAR`，**不会产生带扫描码的虚拟键 down/up**。记事本能收到字符，但读取原始输入的
游戏（精英危险）完全无反应。这是 RustDesk 的 issue
[#8959](https://github.com/rustdesk/rustdesk/issues/8959)。

EdKeyBridge 安装低级键盘钩子，吞掉这些 `VK_PACKET` 的 Unicode 事件，再以可配置的最小
按下时长重新注入真正的 `vkCode`/`scanCode` 按键事件，避免游戏一帧扫不到而丢键。

## 功能

- 纯 Win32 GUI —— 无第三方库，仅用 `syscall`
- 双语界面（中文 / 英文），自动检测系统语言
- TOML 配置（`edkeybridge.toml`）—— 零依赖最小解析器
- 按键分组：数字 (0-9) / 字母 (A-Z) / 特殊按键
- 启动自启、详细日志、诊断（仅观察不转换）模式
- 内嵌程序图标 + 现代 `comctl32` v6 视觉样式
- 完全离线，无网络面板

## 编译

需要 **Go 1.23+** 与 **UPX**。

```powershell
powershell -File build.ps1
```

或手动：

```bash
go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico icon/app.ico -arch amd64 -o src/rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -H windowsgui" -o edkeybridge.exe ./src
upx --best --lzma edkeybridge.exe
```

> 仓库已提交 `rsrc_windows_amd64.syso`（含清单 + 图标），所以一条 `go build` 也能得到
> 带现代主题的 exe。

## 配置

`edkeybridge.toml` 与 exe 同目录，所有项均可选（括号内为默认值）：

| 键 | 默认值 | 含义 |
|----|--------|------|
| `lang` | `auto` | `auto`（跟随系统）/ `zh` / `en` |
| `keys_digits` | `true` | 转换数字 0-9 |
| `keys_letters` | `true` | 转换字母 A-Z |
| `keys_special` | `true` | 转换特殊按键（空格/符号等） |
| `hold_ms` | `80` | 最小按下时长 (ms) |
| `reinject` | `true` | 对注入的真实按键也吞掉 + 重发 |
| `scancode` | `false` | 用扫描码注入而非虚拟键码 |
| `diagnostic` | `false` | 诊断模式：只观察不转换 |
| `autostart` | `false` | 启动即开始桥接 |
| `verbose` | `false` | 详细日志 |

旧版单字段 `keys = "..."` 会在加载时自动迁移。

## 使用

1. 运行 `edkeybridge.exe`。
2. 点击 **开始 / Start**（默认不桥接，需手动开启）。
3. 让《精英危险》保持为前台窗口。来自 RustDesk 安卓端的按键即变为真实物理键。

## 说明

- 建议以管理员身份运行：低级钩子无需管理员也能工作，但部分游戏在更底层截获输入。
- 这是针对 RustDesk 限制的变通方案，并非替代 RustDesk。

## 许可证

[MIT](LICENSE) © 2026 yafeng
