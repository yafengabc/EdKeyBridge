//go:build windows

package main

import (
	"fmt"
	"log"
	"path/filepath"
	"syscall"
	"unsafe"
)

// 提权相关：检测到前台进程（游戏）完整性级别更高时，用 runas 动词触发 UAC 重启本程序。
// UIPI 会静默丢弃低权限进程发往高权限进程的 SendInput，不提权就只能看着键"发不出去"。

var (
	pShellExecuteW      = shell32.NewProc("ShellExecuteW")
	pGetModuleFileNameW = kernel32.NewProc("GetModuleFileNameW")
	pMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	pFlashWindowEx      = user32.NewProc("FlashWindowEx")
	pBringWindowToTop   = user32.NewProc("BringWindowToTop")
	pAttachThreadInput  = user32.NewProc("AttachThreadInput")
	pGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

// FLASHWINFO：闪烁任务栏按钮用。
type flashWInfo struct {
	cbSize    uint32
	hwnd      uintptr
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

const (
	FLASHW_ALL       = 0x00000003 // 同时闪标题栏与任务栏
	FLASHW_TIMERNOFG = 0x0000000C // 一直闪到窗口被激活
)

// flashTaskbar 让任务栏按钮闪烁。
// untilForeground=true 时持续闪到用户点开窗口为止 —— 提权重启后新实例也躲在游戏后面，
// 不闪一下用户根本不知道程序已经重启成功了。
func flashTaskbar(hwnd uintptr, untilForeground bool) {
	if hwnd == 0 {
		return
	}
	flags := uint32(FLASHW_ALL)
	count := uint32(5)
	if untilForeground {
		flags |= FLASHW_TIMERNOFG
		count = 0
	}
	f := flashWInfo{
		cbSize:  uint32(unsafe.Sizeof(flashWInfo{})),
		hwnd:    hwnd,
		dwFlags: flags,
		uCount:  count,
	}
	pFlashWindowEx.Call(uintptr(unsafe.Pointer(&f)))
}

// MONITORINFO：cbSize + rcMonitor + rcWork + dwFlags
type monitorInfo struct {
	cbSize    uint32
	rcMonitor [4]int32
	rcWork    [4]int32
	dwFlags   uint32
}

// elevateDone 保证一次运行内只自动询问一次：
// 用户取消 UAC 或选"否"后不再反复弹窗骚扰（日志里会给出手动提权的办法）。
var elevateDone bool

// moduleFileName 取本程序 exe 的完整路径（提权重启时要重新执行它）。
func moduleFileName() string {
	buf := make([]uint16, 4096)
	n, _, _ := pGetModuleFileNameW.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// canElevate 判断本程序是否还有提升空间（已经是 high/system 就没必要再提）。
func canElevate(selfIL string) bool {
	switch selfIL {
	case "high", "system", "protected":
		return false
	}
	return true
}

// elevateSelf 以管理员身份重新启动本程序（弹出 UAC 提示）。
// ShellExecuteW 返回值 >32 表示成功；≤32 是错误码（常见 5 = 用户取消）。
// 参数里带 -resume：新实例据此"无条件自动开始桥接"，不再依赖配置文件里的 autostart
// （用户可能把 autostart 存成了 false，那样提权重启后就停在"已停止"，等于白提权）。
// 同时显式指定 lpDirectory 为 exe 所在目录：runas 启动的进程默认工作目录是 System32，
// 不指定的话任何相对路径的配置/日志读写都会落到错误的地方。
func elevateSelf() (ok bool, code uintptr) {
	exe := moduleFileName()
	if exe == "" {
		return false, 0
	}
	r, _, _ := pShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(utf16("runas"))),
		uintptr(unsafe.Pointer(utf16(exe))),
		uintptr(unsafe.Pointer(utf16("-resume"))),
		uintptr(unsafe.Pointer(utf16(filepath.Dir(exe)))),
		SW_SHOWNORMAL,
	)
	return r > 32, r
}

// isFullscreen 判断窗口是否铺满了它所在的显示器。
// 只比对主屏是不够的：挂机时游戏常开在副屏，那样判断会漏掉。
func isFullscreen(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
		return false
	}
	var rc [4]int32
	if r, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc[0]))); r == 0 {
		return false
	}
	// 取窗口所在显示器的矩形（MONITOR_DEFAULTTONEAREST = 2）
	hMon, _, _ := pMonitorFromWindow.Call(hwnd, 2)
	var mi monitorInfo
	if hMon != 0 {
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := pGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			mw := mi.rcMonitor[2] - mi.rcMonitor[0]
			mh := mi.rcMonitor[3] - mi.rcMonitor[1]
			if mw > 0 && mh > 0 {
				return (rc[2]-rc[0]) >= mw*95/100 && (rc[3]-rc[1]) >= mh*95/100
			}
		}
	}
	sw, sh := sysMetrics(SM_CXSCREEN), sysMetrics(SM_CYSCREEN)
	if sw <= 0 || sh <= 0 {
		return false
	}
	return (rc[2]-rc[0]) >= sw*95/100 && (rc[3]-rc[1]) >= sh*95/100
}

// bringToFront 把本程序整个窗口拉到前台，返回是否真的抢到了前台。
// 后台进程直接 SetForegroundWindow 通常无效（系统限制前台切换），
// 所以先把输入队列挂到前台线程上（AttachThreadInput）再切，成功率最高。
func bringToFront() bool {
	if g.hwnd == 0 {
		return false
	}
	if r, _, _ := pIsIconic.Call(g.hwnd); r != 0 {
		pShowWindow.Call(g.hwnd, SW_RESTORE)
	}
	pShowWindow.Call(g.hwnd, SW_SHOWNORMAL)

	fg, _, _ := pGetForegroundWindow.Call()
	if fg != 0 && fg != g.hwnd {
		fgThread, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
		myThread, _, _ := pGetCurrentThreadId.Call()
		if fgThread != 0 && myThread != 0 && fgThread != myThread {
			if r, _, _ := pAttachThreadInput.Call(myThread, fgThread, 1); r != 0 {
				defer pAttachThreadInput.Call(myThread, fgThread, 0)
			}
			pBringWindowToTop.Call(g.hwnd)
			pSetForegroundWindow.Call(g.hwnd)
		}
	}
	// 先置顶再**立刻**落回普通层级：置顶这一下足以让窗口浮到最上，
	// 但必须紧接着撤掉，否则窗口会永久挂 WS_EX_TOPMOST 一直压着游戏。
	pSetWindowPos.Call(g.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
	pSetWindowPos.Call(g.hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
	pSetForegroundWindow.Call(g.hwnd)
	pFlashWindow.Call(g.hwnd, 1)
	return isForeground(g.hwnd)
}

// isForeground 判断给定窗口是否处于前台。
func isForeground(hwnd uintptr) bool {
	h, _, _ := pGetForegroundWindow.Call()
	return h != 0 && h == hwnd
}

// clearTopmost 撤掉窗口的"总在最前"属性（幂等，无副作用）。
// 这是防呆：只要有任何路径把窗口设成 HWND_TOPMOST 忘了撤，
// 窗口就会永久压在游戏上面，很难一眼看出是哪次调用漏的。
func clearTopmost() {
	if g.hwnd == 0 {
		return
	}
	pSetWindowPos.Call(g.hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
}

// minimizeBlocking 若前台是全屏窗口（通常是游戏），临时最小化它，返回还原函数。
// 不这么做的话对话框和 UAC 提示都会落在游戏后面，用户根本看不到、也就点不到。
func minimizeBlocking() (restore func()) {
	nop := func() {}
	hwnd, _, _ := pGetForegroundWindow.Call()
	if hwnd == 0 || hwnd == g.hwnd {
		return nop
	}
	// 判据只要"铺满所在显示器"：独占全屏的 D3D 窗口绕过 DWM，
	// 任何 GDI 窗口（包括 MB_TOPMOST 的对话框和 UAC 提示）都会被它埋掉，
	// 只有让它退出全屏才看得见。
	if !isFullscreen(hwnd) {
		return nop
	}
	pShowWindow.Call(hwnd, SW_MINIMIZE)
	log.Println(T("elevate_minimize"))
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		pShowWindow.Call(hwnd, SW_RESTORE)
	}
}

// askElevate 在检测到前台进程权限更高时询问是否提权重启；同意则拉起 UAC 并让位退出。
func askElevate(selfIL, foreIL, fgTitle string) {
	if elevateDone || !canElevate(selfIL) {
		return
	}
	elevateDone = true

	// 先把本程序整个调到前台。抢不到前台（多半是被独占全屏的游戏压住）时，
	// 才让游戏临时最小化让路；无论哪条路结束，都会把游戏还原回去。
	restore := func() {}
	if !bringToFront() {
		restore = minimizeBlocking()
		bringToFront()
	}
	defer func() {
		// 无论走哪条分支都撤掉置顶：留着它窗口会一直压在游戏上面
		clearTopmost()
		restore()
	}()

	// 对话框挂在本程序窗口下（parent=hwnd），跟着主窗口一起浮在最前面
	msg := fmt.Sprintf(T("elevate_ask"), fgTitle, foreIL, selfIL)
	if mb(g.hwnd, msg, T("app_title"),
		MB_YESNO|MB_ICONWARNING|MB_TOPMOST|MB_SETFOREGROUND|MB_SYSTEMMODAL) != IDYES {
		log.Println(T("elevate_declined"))
		return
	}

	// 先把当前界面上的设置落盘，提权后的新实例才能用同一套设置继续桥接
	applyControlsToCfg()
	if err := saveSettings(configPath(), cfg); err != nil {
		log.Println(T("save") + ": " + err.Error())
	}

	log.Println(T("elevate_launch"))
	if ok, code := elevateSelf(); !ok {
		// 错误码很关键：5 = 用户取消/被策略拦（UAC 没弹或被拒绝），
		// 2/3 = exe 路径找不到（配置文件同目录的 exe 被移动或删除）。
		log.Printf(T("elevate_failcode"), code)
		log.Println(T("elevate_fail"))
		mb(g.hwnd, T("elevate_fail"), T("app_title"), MB_OK|MB_ICONWARNING|MB_TOPMOST|MB_SETFOREGROUND)
		return
	}
	// 提权实例已起来，本实例让位退出（defer 会把游戏窗口还原回去）。
	// 直接 DestroyWindow 而不走 WM_CLOSE —— 否则会再弹一次"确认退出"。
	if running {
		stopBridge()
	}
	pDestroyWindow.Call(g.hwnd)
}
