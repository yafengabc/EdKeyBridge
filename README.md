# EdKeyBridge

Bridge keystrokes sent from the **RustDesk Android client** to a **Windows** host
into real physical key events that **Elite Dangerous** can actually read.

[中文文档 / Chinese](README.zh-CN.md) ·
[Changelog](CHANGELOG.md) · [更新日志](CHANGELOG.zh-CN.md)

## Why this exists

RustDesk's Android client does not compile the desktop keyboard pipeline. It
delivers keystrokes to the Windows host as a Unicode text sequence, which the
controlled client injects via `SendInput(KEYEVENTF_UNICODE)`. That produces only
`WM_CHAR` — never a virtual-key `down`/`up` carrying a scan code. Notepad receives
the characters; games that read raw input (Elite Dangerous) see nothing. This is
RustDesk issue [#8959](https://github.com/rustdesk/rustdesk/issues/8959).

EdKeyBridge installs a low-level keyboard hook, swallows those `VK_PACKET`
Unicode events, and re-injects genuine `vkCode`/`scanCode` key events — with a
configurable minimum hold time so fast game frames don't miss the key.

## Features

- Pure Win32 GUI — no third-party libraries, `syscall` only
- Bilingual UI (中文 / English) with automatic system-locale detection
- TOML config (`edkeybridge.toml`) — zero-dependency parser
- Key groups: digits (0-9) / letters (A-Z) / special keys
- Auto-start, verbose logging, diagnostic (observe-only) mode
- Embedded app icon + modern `comctl32` v6 visual styles
- Fully offline; no network panel

## Build

Requires **Go 1.23+** and **UPX**.

```powershell
powershell -File build.ps1
```

Or manually:

```bash
go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -ico icon/app.ico -arch amd64 -o rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -H windowsgui" -o edkeybridge.exe .
upx --best --lzma edkeybridge.exe
```

> The committed `rsrc_windows_amd64.syso` already embeds the manifest + icon, so a
> plain `go build` also yields a themed executable.

## Configuration

`edkeybridge.toml` lives next to the executable. All keys are optional; defaults
shown:

| Key | Default | Meaning |
|-----|---------|---------|
| `lang` | `auto` | `auto` (follow system) / `zh` / `en` |
| `keys_digits` | `true` | Convert digits 0-9 |
| `keys_letters` | `true` | Convert letters A-Z |
| `keys_special` | `true` | Convert special keys (space/symbols) |
| `hold_ms` | `80` | Minimum key-down duration (ms) |
| `reinject` | `true` | Swallow + re-inject injected real keys |
| `scancode` | `false` | Inject via scan code instead of virtual key |
| `diagnostic` | `false` | Observe only, do not transform |
| `autostart` | `false` | Begin bridging on launch |
| `verbose` | `false` | Verbose logging |

Legacy single `keys = "..."` entries are migrated automatically on load.

## Usage

1. Run `edkeybridge.exe`.
2. Click **开始 / Start**. The bridge is off by default.
3. Keep Elite Dangerous as the foreground window. Keystrokes from the RustDesk
   Android client become real physical keys.

## Notes

- Administrator privileges are recommended: a low-level hook works without them,
  but some games capture input at a lower level.
- This is a workaround for a RustDesk limitation, not a replacement for it.

## License

[MIT](LICENSE) © 2026 yafeng
