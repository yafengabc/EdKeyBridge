//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// 系统托盘图标（Shell_NotifyIconW）。
// 目的：程序最小化后仍能从托盘调回来 —— 挂机时窗口常年在游戏后面，
// 没有托盘就只能靠 Alt+Tab 找，而 ED 全屏时 Alt+Tab 也未必好使。

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_INFO    = 0x00000010

	NIIF_INFO = 0x00000001

	// WM_TRAYICON 是托盘回调消息；lParam 低字是鼠标消息（WM_LBUTTONUP 等）。
	WM_TRAYICON      = WM_USER + 20
	WM_LBUTTONUP     = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205

	// 托盘右键菜单命令
	IDM_TRAY_OPEN   = 4001
	IDM_TRAY_TOGGLE = 4002
	IDM_TRAY_QUIT   = 4003

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800

	TPM_LEFTALIGN   = 0x0000
	TPM_RIGHTBUTTON = 0x0002
	TPM_NONOTIFY    = 0x0080
	TPM_RETURNCMD   = 0x0100

	SIZE_MINIMIZED = 1

	SW_HIDE = 0

	WM_NULL = 0x0000
)

// traySelfTest 输出 NOTIFYICONDATA 的大小，供 -selftest 校验结构体布局。
// 这个尺寸错了 Shell_NotifyIconW 会静默失败（托盘图标压根不出现），
// 而无桌面的环境里看不出来，所以把它做成可自检的数值。
func traySelfTest() string {
	return fmt.Sprintf("NOTIFYICONDATA size = %d (x64 expect 976)", unsafe.Sizeof(notifyIconData{}))
}

var (
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenuW      = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pPostMessageW     = user32.NewProc("PostMessageW")
)

// notifyIconData 对应 NOTIFYICONDATAW（x64 下 976 字节）。
// 字段顺序与对齐必须和 Win32 定义一致，否则 Shell_NotifyIconW 会读错。
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32 // 与 uTimeout 共用同一位置
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

var trayAdded bool

// trayIconSm 保存注册托盘用的小图标句柄（由 createGUI 从资源里载入）。
var trayIconSm uintptr

func newNotifyIconData(flags uint32) *notifyIconData {
	nid := &notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             g.hwnd,
		uID:              1,
		uFlags:           flags,
		uCallbackMessage: WM_TRAYICON,
		hIcon:            trayIconSm,
	}
	copy(nid.szTip[:], utf16n(T("app_title"), len(nid.szTip)))
	return nid
}

// utf16n 把字符串拷进定长 UTF-16 缓冲区（必须留结尾的 0）。
func utf16n(s string, n int) []uint16 {
	buf := make([]uint16, n)
	w := syscall.StringToUTF16(s)
	copy(buf, w)
	if len(w) < n {
		buf[len(w)] = 0
	} else {
		buf[n-1] = 0
	}
	return buf
}

// addTrayIcon 注册托盘图标（程序启动时调用一次）。
func addTrayIcon() {
	if g.hwnd == 0 || trayIconSm == 0 {
		return
	}
	nid := newNotifyIconData(NIF_MESSAGE | NIF_ICON | NIF_TIP)
	if r, _, _ := pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(nid))); r != 0 {
		trayAdded = true
	}
}

// removeTrayIcon 退出前摘掉托盘图标（不摘的话图标会一直残留在托盘区）。
func removeTrayIcon() {
	if !trayAdded {
		return
	}
	nid := newNotifyIconData(0)
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(nid)))
	trayAdded = false
}

// showTrayBalloon 弹一条气泡提示（首次最小化到托盘时告诉用户去哪儿找程序）。
func showTrayBalloon(title, msg string) {
	if !trayAdded {
		return
	}
	nid := newNotifyIconData(NIF_INFO)
	copy(nid.szInfoTitle[:], utf16n(title, len(nid.szInfoTitle)))
	copy(nid.szInfo[:], utf16n(msg, len(nid.szInfo)))
	nid.dwInfoFlags = NIIF_INFO
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(nid)))
}

// hideToTray 最小化时把窗口藏起来，只在托盘留图标。
// 不隐藏的话任务栏上还占一个按钮，而窗口本来就被游戏盖着，没意义。
func hideToTray() {
	pShowWindow.Call(g.hwnd, SW_HIDE)
	if !trayHintShown {
		trayHintShown = true
		showTrayBalloon(T("app_title"), T("tray_minimized"))
	}
}

var trayHintShown bool

// restoreFromTray 从托盘恢复窗口。
func restoreFromTray() {
	pShowWindow.Call(g.hwnd, SW_RESTORE)
	pShowWindow.Call(g.hwnd, SW_SHOWNORMAL)
	bringToFront()
}

// showTrayMenu 在鼠标位置弹出托盘菜单。
// 用 TPM_RETURNCMD 直接拿命令 ID，省掉一层 WM_COMMAND 路由。
func showTrayMenu() {
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt[0])))
	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer pDestroyMenu.Call(hMenu)

	addItem := func(id uintptr, s string) {
		pAppendMenuW.Call(hMenu, MF_STRING, id, uintptr(unsafe.Pointer(utf16(s))))
	}
	addItem(IDM_TRAY_OPEN, T("tray_open"))
	if running {
		addItem(IDM_TRAY_TOGGLE, T("stop"))
	} else {
		addItem(IDM_TRAY_TOGGLE, T("start"))
	}
	pAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	addItem(IDM_TRAY_QUIT, T("quit"))

	// 弹出前必须先把窗口带到前台，否则点空白处菜单不会消失（经典 Win32 坑）
	pSetForegroundWindow.Call(g.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(hMenu,
		TPM_LEFTALIGN|TPM_RIGHTBUTTON|TPM_NONOTIFY|TPM_RETURNCMD,
		uintptr(pt[0]), uintptr(pt[1]), 0, g.hwnd, 0)
	pPostMessageW.Call(g.hwnd, WM_NULL, 0, 0) // 收尾，避免菜单留下残影

	switch int(cmd) {
	case IDM_TRAY_OPEN:
		restoreFromTray()
	case IDM_TRAY_TOGGLE:
		onStartStop()
	case IDM_TRAY_QUIT:
		restoreFromTray()
		onQuit()
	}
}

// handleTrayMessage 处理托盘图标上的鼠标事件。
func handleTrayMessage(lParam uintptr) {
	switch lParam & 0xFFFF {
	case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
		restoreFromTray()
	case WM_RBUTTONUP:
		showTrayMenu()
	}
}
