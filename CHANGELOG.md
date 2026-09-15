# Changelog

All notable changes to EdKeyBridge are documented here.
(中文版本见 [CHANGELOG.zh-CN.md](CHANGELOG.zh-CN.md))

## [v0.1.2] - 2026-09-16

### Changed

- **The repository is now 100% Go.** `build.sh` (Shell) and `tools/make_icon.py` (Python) are
  gone; everything executable is Go.
- **`go run build.go`** replaces `build.sh`: it runs rsrc (manifest + icon) → `go build` → UPX
  (UPX optional — the build still succeeds without it). `go run build.go icon` regenerates
  `icon/app.ico` and `icon/preview.png`.
- **Sources moved into `src/`.** All 12 `.go` files plus `rsrc_windows_amd64.syso` now live in
  `src/`, so the project root only holds docs, `build.go`, `icon/`, `tools/` and `go.mod`.
  The `.syso` has to stay in the package directory — Go only links resources found there.

### Fixed

- **Version stamping never worked.** `-ldflags -X` only accepts `main.<var>` for a main package;
  `-X edkeybridge.version` (and `edkeybridge/src.version` after the move) fails silently, so
  every release build reported `dev` instead of its tag. Now `-X main.version=...`, and
  `-selftest` prints the version so a broken path is visible immediately.

### Removed

- `app.rc` — leftover windres resource script, unused since the switch to rsrc.
- `simulate_android.ps1` — early helper for simulating Android-side Unicode injection.
- Dead code: `runMessageLoop()` in `src/bridge.go` and unused constants
  (`SW_SHOW`, `WM_GETTEXT`, `WM_GETTEXTLENGTH`, `WM_SETTEXT`, `NIIF_NONE`).

## [v0.1.1] - 2026-09-16

### Added

- **System tray icon** — the app now lives in the tray. Minimizing hides the window and its
  taskbar button; single/double click the tray icon restores it, right click opens a menu
  (Open / Start-Stop / Quit). A balloon hint on the first minimize tells you where to find it.
- **Privilege (integrity level) self-check** — the window title shows the privilege level this
  app runs at, and the status bar shows the foreground window's level. When the game outranks
  this app (UIPI would silently drop injected keys) it is flagged with a warning.
- **Automatic UAC elevation** — when a higher-privilege foreground window is detected, the app
  offers to restart itself as administrator. Before asking it pulls its own window to the front;
  if a full-screen game still buries the dialog, the game is temporarily minimized and restored
  afterwards.
- **Seamless resume after elevated restart** — the restarted instance always resumes bridging
  (`-resume`), regardless of the `autostart` setting, and flashes its taskbar button until you
  open it (the new window is otherwise hidden behind the game).
- **Config is saved on exit** — previously only the Save button and the elevation path wrote
  `edkeybridge.toml`; changing a setting and closing the window silently discarded it.
- The config file path is printed in the log at startup.

### Fixed

- Checkboxes could not be ticked (`BS_CHECKBOX` does not toggle itself — now `BS_AUTOCHECKBOX`).
- Log pane did not wrap: Go's `log` emits bare LF while the Windows EDIT control needs CRLF.
- Status bar showed garbled Chinese (`SB_SETTEXTA` was used; now `SB_SETTEXTW`).
- `ShellExecuteW(runas)` was launched without a working directory, so the elevated instance
  started with the CWD set to `System32`.
- Elevated restart no longer depends on the saved `autostart` value, which could leave the
  app sitting in "Stopped" right after elevating.
- Elevation failures now log the `ShellExecute` return code (5 = UAC cancelled/blocked,
  2/3 = invalid exe path) instead of failing silently.

### Changed

- Bridging starts automatically on launch by default (`autostart = true`).
- Status and foreground window moved from the main area to a bottom status bar, freeing space
  for the log.
- UI polish: Tahoma 9pt for the English UI, "Min hold time" moved to its own row, labels
  vertically aligned with their inputs, log pane fills the remaining space.

## [v0.1.0] - 2026-09-15

- First release: Win32 GUI, RustDesk Android→Windows key bridging for Elite Dangerous,
  Chinese/English UI with automatic detection, TOML config, embedded icon and manifest.
