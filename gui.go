//go:build windows

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Win32 控件/消息常量
const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_BORDER           = 0x00800000
	WS_CLIPCHILDREN     = 0x02000000
	WS_EX_CLIENTEDGE    = 0x00000200

	ES_AUTOHSCROLL = 0x0080
	ES_MULTILINE   = 0x0004
	ES_READONLY    = 0x0800
	ES_AUTOVSCROLL = 0x0040
	WS_VSCROLL     = 0x00200000

	BS_PUSHBUTTON = 0x0000
	BS_CHECKBOX   = 0x0002
	// BS_AUTOCHECKBOX 点击时自动切换勾选状态；
	// BS_CHECKBOX 只发 BN_CLICKED 通知、不会自动切换，必须由父窗口手动 toggle。
	BS_AUTOCHECKBOX = 0x0003

	CBS_DROPDOWNLIST = 0x0003
	CBS_HASSTRINGS   = 0x0002

	SS_LEFT = 0x0000
	// SS_CENTERIMAGE 让静态文本在控件矩形内垂直居中。
	// 不加它时文本顶对齐，和带边框的 EDIT/COMBOBOX 放同一行会明显错位。
	SS_CENTERIMAGE = 0x0200

	WM_COMMAND       = 0x0111
	WM_SIZE          = 0x0005
	WM_CLOSE         = 0x0010
	WM_DESTROY       = 0x0002
	WM_TIMER         = 0x0113
	WM_GETTEXT       = 0x000D
	WM_GETTEXTLENGTH = 0x000E
	WM_SETFONT       = 0x0030
	WM_SETTEXT       = 0x000C

	BN_CLICKED    = 0
	CBN_SELCHANGE = 1

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_CHECKED = 0x0001

	CB_ADDSTRING = 0x0143
	CB_GETCURSEL = 0x0147
	CB_SETCURSEL = 0x014E

	EM_SETSEL       = 0x00B1
	EM_REPLACESEL   = 0x00C2
	EM_SCROLLCARET  = 0x00B7
	EM_GETLINECOUNT = 0x00BA
	EM_LINEINDEX    = 0x00BB

	// 状态栏（msctls_statusbar32）
	WM_USER = 0x0400
	// 必须用 **W 版**消息号：WM_USER+1 是 SB_SETTEXTA（ANSI 版），传 UTF-16 指针会被
	// 按单字节读取，遇到 ASCII 字符的高位 0x00 就截断（实测 "状态: 运行中" → "状怠:"）。
	SB_SETTEXTW     = WM_USER + 11 // 0x040B
	SB_SETPARTS     = WM_USER + 4  // 0x0404
	SB_SETMINHEIGHT = WM_USER + 8
	SBARS_SIZEGRIP  = 0x0100 // 右下角缩放把手
	ICC_BAR_CLASSES = 0x0004 // InitCommonControlsEx：工具栏/状态栏/跟踪条
	STATUS_CLASS    = "msctls_statusbar32"

	// holdRow 是「最小按下时长」所在行的 y 坐标（标签与输入框共用，保证对齐）。
	holdRow = 214

	// logTop 是日志编辑框的顶边 y 坐标；其高度由 layoutLog() 按客户区剩余空间动态计算。
	logTop = 304
	// sbHeight 是底部状态栏高度；日志底部要为它留出空间。
	sbHeight = 24

	IMAGE_ICON  = 1
	LR_SHARED   = 0x8000
	SM_CXICON   = 11
	SM_CYICON   = 12
	SM_CXSMICON = 49
	SM_CYSMICON = 50

	SW_SHOW = 5

	MB_OK           = 0x00000000
	MB_YESNO        = 0x00000004
	MB_ICONWARNING  = 0x00000030
	MB_ICONQUESTION = 0x00000020
	// MB_SETFOREGROUND / MB_TOPMOST / MB_SYSTEMMODAL：
	// 全屏游戏会盖住普通 MessageBox，必须置顶否则用户根本看不到（表现为"没弹窗"）。
	MB_SETFOREGROUND = 0x00010000
	MB_TOPMOST       = 0x00040000
	MB_SYSTEMMODAL   = 0x00001000
	IDYES            = 6

	CW_USEDEFAULT = 0x80000000

	SW_SHOWNORMAL = 1
	SW_MINIMIZE   = 6
	SW_RESTORE    = 9

	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2
	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_SHOWWINDOW = 0x0040

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	IDC_LANG_LABEL = 200
	IDC_LANG       = 100
	IDC_SETTINGS   = 201
	IDC_KEYS_LABEL = 202
	IDC_DIGITS     = 101
	IDC_LETTERS    = 102
	IDC_SPECIAL    = 103
	IDC_KEYS_HINT  = 203
	IDC_HOLD_LABEL = 204
	IDC_HOLD       = 104
	IDC_REINJECT   = 105
	IDC_SCANCODE   = 106
	IDC_DIAG       = 107
	IDC_AUTOSTART  = 108
	IDC_VERBOSE    = 109
	IDC_START      = 120
	IDC_SAVE       = 121
	IDC_QUIT       = 122
	IDC_LOG_LABEL  = 209
	IDC_LOG        = 130
	IDC_STATUSBAR  = 140

	// 定时器 ID
	timerForeground   = 1 // 刷新前台窗口/权限检测
	timerBringToFront = 2 // 提权重启后延迟把窗口抬到前台
)

// guiT 保存全部控件句柄，便于语言切换与状态刷新时统一操作。
type guiT struct {
	hwnd       uintptr
	hLog       uintptr
	hStatusBar uintptr
	hStart     uintptr
	hSave      uintptr
	hQuit      uintptr
	hLang      uintptr
	hDigits    uintptr
	hLetters   uintptr
	hSpecial   uintptr
	hHold      uintptr
	hReinject  uintptr
	hScancode  uintptr
	hDiag      uintptr
	hAutostart uintptr
	hVerbose   uintptr
	hLangLabel uintptr
	hSettings  uintptr
	hKeysLabel uintptr
	hKeysHint  uintptr
	hHoldLabel uintptr
	hLogLabel  uintptr
}

var g guiT

var (
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pSendMessageW         = user32.NewProc("SendMessageW")
	pShowWindow           = user32.NewProc("ShowWindow")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pSetTimer             = user32.NewProc("SetTimer")
	pKillTimer            = user32.NewProc("KillTimer")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pLoadCursor           = user32.NewProc("LoadCursorW")
	pGetStockObject       = gdi32.NewProc("GetStockObject")
	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	pLoadImageW           = user32.NewProc("LoadImageW")
	pGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pMoveWindow           = user32.NewProc("MoveWindow")
	pCreateFontW          = gdi32.NewProc("CreateFontW")
	pGetDC                = user32.NewProc("GetDC")
	pReleaseDC            = user32.NewProc("ReleaseDC")
	pGetDeviceCaps        = gdi32.NewProc("GetDeviceCaps")
	pSelectObject         = gdi32.NewProc("SelectObject")
	pGetTextExtentPoint32 = gdi32.NewProc("GetTextExtentPoint32W")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pFlashWindow          = user32.NewProc("FlashWindow")
	pIsIconic             = user32.NewProc("IsIconic")
	pGetWindowRect        = user32.NewProc("GetWindowRect")
)

var fontH uintptr

// 英文用 Tahoma（比默认的 MS Shell Dlg 窄，同样宽度能多放几个字符）；
// 中文仍用系统默认字体 —— Tahoma 没有 CJK 字形，强制使用会走 fallback（一般是宋体），观感变差。
var (
	fontEn uintptr
	fontZh uintptr
)

// fontSizePt 是英文模式下的字号（pt）。中文字体仍用系统默认，不走这里。
// 9pt：比系统默认 UI 的 8pt 大一档，英文版看起来不至于太小；Tahoma 本身偏窄，
// 放大后仍比默认字体省宽度。
const fontSizePt = 9

// makeTahoma 创建 DPI 感知的 Tahoma（字形窄，同样宽度能多放几个字符）。
func makeTahoma() uintptr {
	const LOGPIXELSY = 90
	hdc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, LOGPIXELSY)
	pReleaseDC.Call(0, hdc)
	if dpi <= 0 {
		dpi = 96
	}
	height := -int32(dpi * fontSizePt / 72) // 负号表示按字符高度而非行高
	h, _, _ := pCreateFontW.Call(
		uintptr(height), 0, 0, 0,
		400, // FW_NORMAL
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0, 0,
		0, // FF_DONTCARE
		uintptr(unsafe.Pointer(utf16("Tahoma"))),
	)
	return h
}

func createUIFonts() {
	fontZh = getGUIFont()
	fontEn = makeTahoma()
	if fontEn == 0 {
		fontEn = fontZh
	}
}

func currentFont() uintptr {
	if curLang == "en" && fontEn != 0 {
		return fontEn
	}
	if fontZh != 0 {
		return fontZh
	}
	return fontH
}

// allControls 返回所有子控件句柄（顺序无关，仅用于统一套字体/刷新）。
func allControls() []uintptr {
	return []uintptr{
		g.hLog, g.hStatusBar, g.hStart, g.hSave, g.hQuit, g.hLang,
		g.hDigits, g.hLetters, g.hSpecial, g.hHold, g.hReinject, g.hScancode,
		g.hDiag, g.hAutostart, g.hVerbose, g.hLangLabel, g.hSettings,
		g.hKeysLabel, g.hKeysHint, g.hHoldLabel, g.hLogLabel,
	}
}

// applyUIFont 按当前语言给所有控件套字体（切换语言时调用）。
func applyUIFont() {
	f := currentFont()
	if f == 0 {
		return
	}
	fontH = f
	for _, h := range allControls() {
		if h != 0 {
			pSendMessageW.Call(h, WM_SETFONT, f, 1)
		}
	}
}

func utf16(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }

func hInstance() uintptr {
	h, _, _ := pGetModuleHandle.Call(0)
	return h
}

func loadCursor() uintptr {
	h, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	return h
}

func getGUIFont() uintptr {
	h, _, _ := pGetStockObject.Call(17) // DEFAULT_GUI_FONT
	return h
}

// sysMetrics 包装 GetSystemMetrics（取系统图标真实尺寸）。
func sysMetrics(n int32) int32 {
	r, _, _ := pGetSystemMetrics.Call(uintptr(n))
	return int32(r)
}

// loadAppIcons 从资源里取应用图标（大/小两档）。
// rsrc 先放清单(ID=1)后放图标(ID=2)，故优先试 2，再试 1。
func loadAppIcons() (hIcon, hIconSm uintptr) {
	inst := hInstance()
	load := func(id uintptr, cx, cy int32) uintptr {
		h, _, _ := pLoadImageW.Call(inst, id, IMAGE_ICON, uintptr(cx), uintptr(cy), LR_SHARED)
		return h
	}
	cx, cy := sysMetrics(SM_CXICON), sysMetrics(SM_CYICON)
	sx, sy := sysMetrics(SM_CXSMICON), sysMetrics(SM_CYSMICON)
	hIcon = load(2, cx, cy)
	if hIcon == 0 {
		hIcon = load(1, cx, cy)
	}
	hIconSm = load(2, sx, sy)
	if hIconSm == 0 {
		hIconSm = hIcon
	}
	return
}

type wndclassex struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type initCommonControlsExT struct {
	DwSize uint32
	DwICC  uint32
}

// createControl 创建子控件，并统一套用 GUI 字体。
func createControl(class, text string, style, exStyle uint32, x, y, w, h int32, parent uintptr, id int) uintptr {
	hw, _, _ := pCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(utf16(class))),
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent,
		uintptr(id),
		hInstance(),
		0,
	)
	if hw != 0 && fontH != 0 {
		pSendMessageW.Call(hw, WM_SETFONT, fontH, 1)
	}
	return hw
}

func setText(hwnd uintptr, s string) {
	pSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16(s))))
}

func getEditText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func isChecked(hwnd uintptr) bool {
	r, _, _ := pSendMessageW.Call(hwnd, BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}

func setChecked(hwnd uintptr, checked bool) {
	v := uintptr(0)
	if checked {
		v = BST_CHECKED
	}
	pSendMessageW.Call(hwnd, BM_SETCHECK, v, 0)
}

func mb(parent uintptr, text, title string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(parent,
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(unsafe.Pointer(utf16(title))),
		uintptr(flags))
	return int(r)
}

// guiLogWriter 把 log 输出追加到只读日志编辑框（跨线程 SendMessage 安全）。
type guiLogWriter struct{ hwnd uintptr }

func (w guiLogWriter) Write(p []byte) (int, error) {
	if w.hwnd == 0 {
		return len(p), nil
	}
	s := string(p)
	// 关键：Windows EDIT 控件只认 CRLF。log 包输出的是裸 LF，
	// 不转换的话控件不会断行，整段文字会挤在"一行"里横向溢出（表现为"日志不换行"）。
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")

	pSendMessageW.Call(w.hwnd, EM_SETSEL, ^uintptr(0), ^uintptr(0))
	pSendMessageW.Call(w.hwnd, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(utf16(s))))
	pSendMessageW.Call(w.hwnd, EM_SETSEL, ^uintptr(0), ^uintptr(0))
	pSendMessageW.Call(w.hwnd, EM_SCROLLCARET, 0, 0)

	trimLog(w.hwnd)
	return len(p), nil
}

// trimLog 限制日志行数，避免长时间挂机（ED 常开十几个小时）把内存吃满。
func trimLog(hwnd uintptr) {
	const maxLines = 800
	const dropLines = 300
	n, _, _ := pSendMessageW.Call(hwnd, EM_GETLINECOUNT, 0, 0)
	if n <= maxLines {
		return
	}
	idx, _, _ := pSendMessageW.Call(hwnd, EM_LINEINDEX, dropLines, 0)
	if idx <= 0 {
		return
	}
	pSendMessageW.Call(hwnd, EM_SETSEL, 0, idx)
	pSendMessageW.Call(hwnd, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(utf16(""))))
	pSendMessageW.Call(hwnd, EM_SETSEL, ^uintptr(0), ^uintptr(0))
}

// createGUI 注册窗口类、创建主窗口与全部控件。
func createGUI() bool {
	icc := initCommonControlsExT{
		DwSize: uint32(unsafe.Sizeof(initCommonControlsExT{})),
		// ICC_STANDARD_CLASSES(编辑框/按钮等) | ICC_BAR_CLASSES(状态栏)
		DwICC: 0x0008 | ICC_BAR_CLASSES,
	}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	createUIFonts()
	fontH = currentFont()

	hIcon, hIconSm := loadAppIcons()
	trayIconSm = hIconSm // 托盘用小图标（16×16）

	className := "EdKeyBridgeWnd"
	wc := wndclassex{
		cbSize:        uint32(unsafe.Sizeof(wndclassex{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInstance(),
		hIcon:         hIcon,
		hIconSm:       hIconSm,
		hCursor:       loadCursor(),
		hbrBackground: 16, // COLOR_BTNFACE + 1
		lpszClassName: utf16(className),
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	h, _, _ := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16(className))),
		uintptr(unsafe.Pointer(utf16(T("app_title")))),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN,
		CW_USEDEFAULT, CW_USEDEFAULT, 470, 632,
		0, 0, hInstance(), 0,
	)
	if h == 0 {
		return false
	}
	g.hwnd = h
	buildControls()
	return true
}

func buildControls() {
	cs := uint32(WS_CHILD | WS_VISIBLE)
	edge := uint32(WS_EX_CLIENTEDGE)

	// 第 1 行只放语言（界面语言不属于"转换设置"，单独置顶）
	// 标签与下拉框同 y、同高，配合 SS_CENTERIMAGE 使文字垂直居中对齐
	g.hLangLabel = createControl("STATIC", T("lang"), cs|SS_LEFT|SS_CENTERIMAGE, 0, 16, 12, 52, 22, g.hwnd, IDC_LANG_LABEL)
	g.hLang = createControl("COMBOBOX", "", cs|CBS_DROPDOWNLIST|CBS_HASSTRINGS, edge, 72, 12, 110, 200, g.hwnd, IDC_LANG)
	for _, s := range []string{"Auto", "中文", "English"} {
		pSendMessageW.Call(g.hLang, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(utf16(s))))
	}

	g.hSettings = createControl("STATIC", T("settings"), cs|SS_LEFT, 0, 16, 46, 200, 20, g.hwnd, IDC_SETTINGS)

	// 按键分组：三个复选框并排一行（原来是各占一整行，右侧大片空白）
	g.hKeysLabel = createControl("STATIC", T("keys"), cs|SS_LEFT, 0, 16, 70, 200, 20, g.hwnd, IDC_KEYS_LABEL)
	g.hDigits = createControl("BUTTON", T("keys_digits"), cs|BS_AUTOCHECKBOX, 0, 16, 90, 138, 22, g.hwnd, IDC_DIGITS)
	setChecked(g.hDigits, cfg.KeysDig)
	g.hLetters = createControl("BUTTON", T("keys_letters"), cs|BS_AUTOCHECKBOX, 0, 160, 90, 138, 22, g.hwnd, IDC_LETTERS)
	setChecked(g.hLetters, cfg.KeysLet)
	g.hSpecial = createControl("BUTTON", T("keys_special"), cs|BS_AUTOCHECKBOX, 0, 304, 90, 142, 22, g.hwnd, IDC_SPECIAL)
	setChecked(g.hSpecial, cfg.KeysSpec)
	// 高度给 20：STATIC 会自动折行，两行时也不至于被裁掉
	g.hKeysHint = createControl("STATIC", T("keys_hint"), cs|SS_LEFT, 0, 16, 116, 430, 20, g.hwnd, IDC_KEYS_HINT)

	// 其余开关：两列网格（5 项 → 3 行，末位留空）
	g.hReinject = createControl("BUTTON", T("reinject"), cs|BS_AUTOCHECKBOX, 0, 16, 140, 210, 22, g.hwnd, IDC_REINJECT)
	setChecked(g.hReinject, cfg.Reinject)
	g.hScancode = createControl("BUTTON", T("scancode"), cs|BS_AUTOCHECKBOX, 0, 236, 140, 210, 22, g.hwnd, IDC_SCANCODE)
	setChecked(g.hScancode, cfg.Scancode)
	g.hDiag = createControl("BUTTON", T("diagnostic"), cs|BS_AUTOCHECKBOX, 0, 16, 164, 210, 22, g.hwnd, IDC_DIAG)
	setChecked(g.hDiag, cfg.Diagnostic)
	g.hAutostart = createControl("BUTTON", T("autostart"), cs|BS_AUTOCHECKBOX, 0, 236, 164, 210, 22, g.hwnd, IDC_AUTOSTART)
	setChecked(g.hAutostart, cfg.Autostart)
	// 第 3 行：详细日志（单独占左侧，右侧留空）
	g.hVerbose = createControl("BUTTON", T("verbose"), cs|BS_AUTOCHECKBOX, 0, 16, 188, 210, 22, g.hwnd, IDC_VERBOSE)
	setChecked(g.hVerbose, cfg.Verbose)

	// 第 4 行：最小按下时长单独一行（它是数值输入，混在勾选框里不协调）
	g.hHoldLabel = createControl("STATIC", T("hold"), cs|SS_LEFT|SS_CENTERIMAGE, 0, 16, holdRow, 140, 22, g.hwnd, IDC_HOLD_LABEL)
	g.hHold = createControl("EDIT", strconv.Itoa(cfg.HoldMs), cs|WS_BORDER|ES_AUTOHSCROLL, edge, 160, holdRow, 100, 22, g.hwnd, IDC_HOLD)

	// 状态与前台窗口已移至底部状态栏，此处不再占两行（省下的空间全给日志）
	g.hStart = createControl("BUTTON", T("start"), cs|BS_PUSHBUTTON, 0, 16, 248, 130, 30, g.hwnd, IDC_START)
	g.hSave = createControl("BUTTON", T("save"), cs|BS_PUSHBUTTON, 0, 168, 248, 130, 30, g.hwnd, IDC_SAVE)
	g.hQuit = createControl("BUTTON", T("quit"), cs|BS_PUSHBUTTON, 0, 320, 248, 126, 30, g.hwnd, IDC_QUIT)

	g.hLogLabel = createControl("STATIC", T("log_title"), cs|SS_LEFT, 0, 16, 286, 200, 18, g.hwnd, IDC_LOG_LABEL)
	// ES_MULTILINE 且不带 ES_AUTOHSCROLL → 自动按词折行（配合 CRLF 才会真正断行）
	g.hLog = createControl("EDIT", "",
		cs|WS_BORDER|ES_MULTILINE|ES_AUTOVSCROLL|WS_VSCROLL|ES_READONLY, edge,
		16, logTop, 430, 60, g.hwnd, IDC_LOG)

	// 底部状态栏：状态 | 前台窗口
	g.hStatusBar = createControl(STATUS_CLASS, "", cs|SBARS_SIZEGRIP, 0,
		0, 0, 0, sbHeight, g.hwnd, IDC_STATUSBAR)
	pSendMessageW.Call(g.hStatusBar, SB_SETMINHEIGHT, sbHeight, 0)

	// 日志高度按客户区剩余空间撑满（不硬编码，避免被非客户区尺寸吃掉）
	layoutStatusBar()
	layoutHold()
	layoutLog()

	applyUIFont()
	updateTitle()
	updateStatus()
	updateForeground()

	pSetTimer.Call(g.hwnd, 1, 500, 0)
}

// layoutStatusBar 把状态栏贴到客户区底部，并按下窗口宽度重算分栏。
// 第 0 栏固定宽度放运行状态，第 1 栏吃掉剩余空间放前台窗口（-1 表示延伸到右边）。
func layoutStatusBar() {
	if g.hStatusBar == 0 || g.hwnd == 0 {
		return
	}
	var r [4]int32
	pGetClientRect.Call(g.hwnd, uintptr(unsafe.Pointer(&r[0])))
	cw, ch := r[2], r[3]
	pMoveWindow.Call(g.hStatusBar, 0, uintptr(ch-sbHeight), uintptr(cw), sbHeight, 1)

	part0 := int32(130)
	if cw > 200 {
		part0 = cw * 28 / 100
	}
	parts := [2]int32{part0, -1}
	pSendMessageW.Call(g.hStatusBar, SB_SETPARTS, 2, uintptr(unsafe.Pointer(&parts[0])))
}

// textWidth 用当前字体测量字符串像素宽度（用于让控件紧贴文字，不留大空隙）。
func textWidth(s string) int32 {
	owner := g.hwnd
	hdc, _, _ := pGetDC.Call(owner)
	if hdc == 0 { // 窗口 DC 拿不到时退到屏幕 DC，仍能测出文字宽度
		owner = 0
		hdc, _, _ = pGetDC.Call(0)
	}
	if hdc == 0 {
		return 0
	}
	old, _, _ := pSelectObject.Call(hdc, currentFont())
	wstr := syscall.StringToUTF16(s)
	var sz [2]int32
	pGetTextExtentPoint32.Call(hdc,
		uintptr(unsafe.Pointer(&wstr[0])), uintptr(len(wstr)-1),
		uintptr(unsafe.Pointer(&sz[0])))
	pSelectObject.Call(hdc, old)
	pReleaseDC.Call(owner, hdc)
	return sz[0]
}

// layoutHold 让「最小按下时长」的输入框紧贴标签文字。
// 固定坐标行不通：中英文文案宽度差不少（约 121px vs 112px），写死间距会在某一种语言下
// 留出一大段空白，所以按当前字体实测文字宽度后摆放。
func layoutHold() {
	if g.hHoldLabel == 0 || g.hHold == 0 {
		return
	}
	w := textWidth(T("hold")) + 6
	if w < 60 { // 测量失败（无 DC 等）时退回一个合理宽度，避免控件叠在一起
		w = 124
	}
	// 标签与输入框同 y、同高；标签带 SS_CENTERIMAGE 会把文字垂直居中，两者基线才一致
	pMoveWindow.Call(g.hHoldLabel, 16, holdRow, uintptr(w), 22, 1)
	pMoveWindow.Call(g.hHold, uintptr(16+w+4), holdRow, 100, 22, 1)
}

// setStatusText 设置状态栏某一栏的文字。
func setStatusText(part int, s string) {
	if g.hStatusBar == 0 {
		return
	}
	pSendMessageW.Call(g.hStatusBar, SB_SETTEXTW, uintptr(part), uintptr(unsafe.Pointer(utf16(s))))
}

// layoutLog 让日志框填满底部剩余空间，随窗口缩放自适应。
func layoutLog() {
	if g.hLog == 0 || g.hwnd == 0 {
		return
	}
	var r [4]int32
	pGetClientRect.Call(g.hwnd, uintptr(unsafe.Pointer(&r[0])))
	cw, ch := r[2], r[3]
	w := cw - 32
	h := ch - logTop - 16 - sbHeight // 底部给状态栏留位置
	if w < 60 {
		w = 60
	}
	if h < 60 {
		h = 60
	}
	pMoveWindow.Call(g.hLog, 16, logTop, uintptr(w), uintptr(h), 1)
}

// applyLang 用当前语言刷新所有控件文案。
func applyLang() {
	applyUIFont() // 语言切换时同步换字体（英文用窄体 Tahoma）
	updateTitle()
	setText(g.hLangLabel, T("lang"))
	setText(g.hSettings, T("settings"))
	setText(g.hKeysLabel, T("keys"))
	setText(g.hDigits, T("keys_digits"))
	setText(g.hLetters, T("keys_letters"))
	setText(g.hSpecial, T("keys_special"))
	setText(g.hKeysHint, T("keys_hint"))
	setText(g.hHoldLabel, T("hold"))
	setText(g.hLogLabel, T("log_title"))
	setText(g.hReinject, T("reinject"))
	setText(g.hScancode, T("scancode"))
	setText(g.hDiag, T("diagnostic"))
	setText(g.hAutostart, T("autostart"))
	setText(g.hVerbose, T("verbose"))
	updateStartLabel()
	setText(g.hSave, T("save"))
	setText(g.hQuit, T("quit"))
	layoutHold() // 换语言后标签长度变了，输入框要重新贴上去
	updateStatus()
	updateForeground() // 状态栏第 2 栏的前缀文案也要跟着换语言
}

// privLabel 把 ilName() 的英文级别名转成本地化短标签（用于标题栏）。
func privLabel(name string) string {
	switch name {
	case "high":
		return T("ilv_high")
	case "medium+":
		return T("ilv_mediump")
	case "medium":
		return T("ilv_medium")
	case "low":
		return T("ilv_low")
	case "system":
		return T("ilv_system")
	case "protected":
		return T("ilv_protect")
	default:
		return T("ilv_unknown")
	}
}

// updateTitle 在标题栏显示本程序权限级别。
// 权限决定 SendInput 能否送达游戏（UIPI），放标题栏可一眼确认是否以管理员运行。
func updateTitle() {
	self, _, _ := ilReport()
	if self == "?" || self == "" {
		setText(g.hwnd, T("app_title"))
		return
	}
	setText(g.hwnd, fmt.Sprintf(T("title_priv"), T("app_title"), privLabel(self)))
}

func updateStartLabel() {
	if running {
		setText(g.hStart, T("stop"))
	} else {
		setText(g.hStart, T("start"))
	}
}

// updateStatus 刷新状态栏第 0 栏：运行中 / 已停止 / 诊断。
// 用短词而非完整描述（诊断模式的完整说明较长，状态栏放不下会被截断）。
func updateStatus() {
	state := T("stopped")
	if running {
		if cfg.Diagnostic {
			state = T("stat_diag")
		} else {
			state = T("running")
		}
	}
	setStatusText(0, T("status")+": "+state)
}

// lastILBlocked 记录上一次的权限告警状态，避免定时器每 500ms 重复刷屏。
var lastILBlocked bool

// updateForeground 刷新前台窗口显示；当前台进程权限高于本程序时告警。
// 该情况会让 SendInput 被 UIPI 静默丢弃（表现：记事本能收到、游戏收不到）。
func updateForeground() {
	self, fore, blocked := ilReport()
	base := foregroundTitle()
	if base == "" {
		base = T("none")
	}
	title := base
	if blocked {
		title += " " + T("il_higher")
	}
	setStatusText(1, T("foreground")+": "+title)
	if blocked != lastILBlocked {
		if blocked {
			log.Printf(T("il_blocked"), fore, self)
			// 权限不足导致按键送不进去 —— 主动询问是否提权重启
			askElevate(self, fore, base)
		}
		lastILBlocked = blocked
	}
}

func setLangCombo() {
	switch cfg.Lang {
	case "zh":
		pSendMessageW.Call(g.hLang, CB_SETCURSEL, 1, 0)
	case "en":
		pSendMessageW.Call(g.hLang, CB_SETCURSEL, 2, 0)
	default:
		pSendMessageW.Call(g.hLang, CB_SETCURSEL, 0, 0)
	}
}

func applyControlsToCfg() {
	cfg.KeysDig = isChecked(g.hDigits)
	cfg.KeysLet = isChecked(g.hLetters)
	cfg.KeysSpec = isChecked(g.hSpecial)
	if n, err := strconv.Atoi(strings.TrimSpace(getEditText(g.hHold))); err == nil {
		cfg.HoldMs = n
	}
	cfg.Reinject = isChecked(g.hReinject)
	cfg.Scancode = isChecked(g.hScancode)
	cfg.Diagnostic = isChecked(g.hDiag)
	cfg.Autostart = isChecked(g.hAutostart)
	cfg.Verbose = isChecked(g.hVerbose)
}

func onLangChange() {
	sel, _, _ := pSendMessageW.Call(g.hLang, CB_GETCURSEL, 0, 0)
	switch int(sel) {
	case 1:
		cfg.Lang = "zh"
	case 2:
		cfg.Lang = "en"
	default:
		cfg.Lang = "auto"
	}
	curLang = resolveLang(cfg.Lang)
	applyLang()
}

func onStartStop() {
	if running {
		stopBridge()
	} else {
		applyControlsToCfg()
		if err := startBridge(); err != nil {
			mb(0, err.Error(), T("app_title"), MB_OK)
		}
	}
	updateStartLabel()
	updateStatus()
}

func onSave() {
	applyControlsToCfg()
	if err := saveSettings(configPath(), cfg); err != nil {
		mb(0, err.Error(), T("app_title"), MB_OK)
		return
	}
	mb(0, T("saved"), T("app_title"), MB_OK)
}

// saveSettingsOnExit 退出前把界面上的设置落盘。
// 原来只有点「保存配置」和提权前会写盘，改了设置直接关窗口就全丢了（语言选择也一样）。
// 所有退出路径最终都汇聚到 WM_DESTROY，放这里做单一出口最稳。
func saveSettingsOnExit() {
	applyControlsToCfg()
	if err := saveSettings(configPath(), cfg); err != nil {
		// exe 在 Program Files 且未提权时会写失败，这里只记日志
		// （退出途中弹框会打断流程，失败也别拦着退出）
		log.Println(T("save") + ": " + err.Error())
	}
}

func onQuit() {
	if mb(0, T("confirm_quit"), T("app_title"), MB_YESNO|MB_ICONQUESTION) == IDYES {
		stopBridge()
		pDestroyWindow.Call(g.hwnd)
	}
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		code := uint16(wParam >> 16)
		id := int(uint16(wParam))
		switch id {
		case IDC_LANG:
			if code == CBN_SELCHANGE {
				onLangChange()
			}
		case IDC_START:
			if code == BN_CLICKED {
				onStartStop()
			}
		case IDC_SAVE:
			if code == BN_CLICKED {
				onSave()
			}
		case IDC_QUIT:
			if code == BN_CLICKED {
				onQuit()
			}
		}
		return 0
	case WM_TRAYICON:
		handleTrayMessage(lParam)
		return 0
	case WM_SIZE:
		// 点最小化：不留在任务栏，收进托盘（游戏全屏时任务栏按钮很难点到）
		if int32(wParam) == SIZE_MINIMIZED {
			hideToTray()
			return 0
		}
		layoutStatusBar()
		layoutLog()
		return 0
	case WM_TIMER:
		switch wParam {
		case timerForeground:
			updateForeground()
		case timerBringToFront: // 提权重启后延迟抢前台（见 main.go 的 -resume）
			pKillTimer.Call(hwnd, timerBringToFront)
			bringToFront()
		}
		return 0
	case WM_CLOSE:
		if mb(0, T("confirm_quit"), T("app_title"), MB_YESNO|MB_ICONQUESTION) == IDYES {
			stopBridge()
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case WM_DESTROY:
		saveSettingsOnExit() // 退出即保存，不依赖用户记得点「保存配置」
		removeTrayIcon()     // 不清的话托盘图标会一直残留
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// runGUI 主消息循环；同时驱动低级键盘钩子（钩子回调运行于本线程）。
func runGUI() {
	var msg winMSG
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
