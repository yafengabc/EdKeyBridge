//go:build windows

package main

import (
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

	CBS_DROPDOWNLIST = 0x0003
	CBS_HASSTRINGS   = 0x0002

	SS_LEFT = 0x0000

	WM_COMMAND      = 0x0111
	WM_CLOSE        = 0x0010
	WM_DESTROY      = 0x0002
	WM_TIMER        = 0x0113
	WM_GETTEXT      = 0x000D
	WM_GETTEXTLENGTH = 0x000E
	WM_SETFONT      = 0x0030
	WM_SETTEXT      = 0x000C

	BN_CLICKED    = 0
	CBN_SELCHANGE = 1

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_CHECKED = 0x0001

	CB_ADDSTRING  = 0x0143
	CB_GETCURSEL  = 0x0147
	CB_SETCURSEL  = 0x014E

	EM_SETSEL      = 0x00B1
	EM_REPLACESEL  = 0x00C2
	EM_SCROLLCARET = 0x00B7

	IMAGE_ICON  = 1
	LR_SHARED   = 0x8000
	SM_CXICON   = 11
	SM_CYICON   = 12
	SM_CXSMICON = 49
	SM_CYSMICON = 50

	SW_SHOW = 5

	MB_OK           = 0x00000000
	MB_YESNO        = 0x00000004
	MB_ICONQUESTION = 0x00000020
	IDYES           = 6

	CW_USEDEFAULT = 0x80000000

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
	IDC_STATUS_LABEL = 207
	IDC_STATUS       = 110
	IDC_FG_LABEL     = 208
	IDC_FG           = 111
	IDC_START  = 120
	IDC_SAVE   = 121
	IDC_QUIT   = 122
	IDC_LOG_LABEL = 209
	IDC_LOG       = 130
)

// guiT 保存全部控件句柄，便于语言切换与状态刷新时统一操作。
type guiT struct {
	hwnd       uintptr
	hLog       uintptr
	hStatus    uintptr
	hFG        uintptr
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
	hStatusLbl uintptr
	hFGLbl     uintptr
	hLogLabel  uintptr
}

var g guiT

var (
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pSetWindowTextW   = user32.NewProc("SetWindowTextW")
	pSendMessageW     = user32.NewProc("SendMessageW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pUpdateWindow     = user32.NewProc("UpdateWindow")
	pMessageBoxW      = user32.NewProc("MessageBoxW")
	pSetTimer         = user32.NewProc("SetTimer")
	pKillTimer        = user32.NewProc("KillTimer")
	pGetWindowTextW   = user32.NewProc("GetWindowTextW")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pLoadCursor       = user32.NewProc("LoadCursorW")
	pGetStockObject   = gdi32.NewProc("GetStockObject")
	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	pLoadImageW       = user32.NewProc("LoadImageW")
	pGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

var fontH uintptr

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
	pSendMessageW.Call(w.hwnd, EM_SETSEL, ^uintptr(0), ^uintptr(0))
	pSendMessageW.Call(w.hwnd, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(utf16(s))))
	pSendMessageW.Call(w.hwnd, EM_SETSEL, ^uintptr(0), ^uintptr(0))
	pSendMessageW.Call(w.hwnd, EM_SCROLLCARET, 0, 0)
	return len(p), nil
}

// createGUI 注册窗口类、创建主窗口与全部控件。
func createGUI() bool {
	icc := initCommonControlsExT{
		DwSize: uint32(unsafe.Sizeof(initCommonControlsExT{})),
		DwICC:  0x0008, // ICC_STANDARD_CLASSES
	}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	fontH = getGUIFont()

	hIcon, hIconSm := loadAppIcons()

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
		CW_USEDEFAULT, CW_USEDEFAULT, 470, 550,
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

	g.hLangLabel = createControl("STATIC", T("lang"), cs|SS_LEFT, 0, 16, 16, 70, 22, g.hwnd, IDC_LANG_LABEL)
	g.hLang = createControl("COMBOBOX", "", cs|CBS_DROPDOWNLIST|CBS_HASSTRINGS, edge, 92, 14, 130, 200, g.hwnd, IDC_LANG)
	for _, s := range []string{"Auto", "中文", "English"} {
		pSendMessageW.Call(g.hLang, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(utf16(s))))
	}

	g.hSettings = createControl("STATIC", T("settings"), cs|SS_LEFT, 0, 16, 46, 200, 20, g.hwnd, IDC_SETTINGS)

	g.hKeysLabel = createControl("STATIC", T("keys"), cs|SS_LEFT, 0, 16, 70, 200, 20, g.hwnd, IDC_KEYS_LABEL)
	g.hDigits = createControl("BUTTON", T("keys_digits"), cs|BS_CHECKBOX, 0, 16, 92, 430, 22, g.hwnd, IDC_DIGITS)
	setChecked(g.hDigits, cfg.KeysDig)
	g.hLetters = createControl("BUTTON", T("keys_letters"), cs|BS_CHECKBOX, 0, 16, 116, 430, 22, g.hwnd, IDC_LETTERS)
	setChecked(g.hLetters, cfg.KeysLet)
	g.hSpecial = createControl("BUTTON", T("keys_special"), cs|BS_CHECKBOX, 0, 16, 140, 430, 22, g.hwnd, IDC_SPECIAL)
	setChecked(g.hSpecial, cfg.KeysSpec)
	g.hKeysHint = createControl("STATIC", T("keys_hint"), cs|SS_LEFT, 0, 16, 166, 430, 18, g.hwnd, IDC_KEYS_HINT)

	g.hHoldLabel = createControl("STATIC", T("hold"), cs|SS_LEFT, 0, 16, 190, 240, 20, g.hwnd, IDC_HOLD_LABEL)
	g.hHold = createControl("EDIT", strconv.Itoa(cfg.HoldMs), cs|WS_BORDER|ES_AUTOHSCROLL, edge, 16, 210, 120, 22, g.hwnd, IDC_HOLD)

	g.hReinject = createControl("BUTTON", T("reinject"), cs|BS_CHECKBOX, 0, 16, 240, 430, 22, g.hwnd, IDC_REINJECT)
	setChecked(g.hReinject, cfg.Reinject)
	g.hScancode = createControl("BUTTON", T("scancode"), cs|BS_CHECKBOX, 0, 16, 264, 430, 22, g.hwnd, IDC_SCANCODE)
	setChecked(g.hScancode, cfg.Scancode)
	g.hDiag = createControl("BUTTON", T("diagnostic"), cs|BS_CHECKBOX, 0, 16, 288, 430, 22, g.hwnd, IDC_DIAG)
	setChecked(g.hDiag, cfg.Diagnostic)
	g.hAutostart = createControl("BUTTON", T("autostart"), cs|BS_CHECKBOX, 0, 16, 312, 430, 22, g.hwnd, IDC_AUTOSTART)
	setChecked(g.hAutostart, cfg.Autostart)
	g.hVerbose = createControl("BUTTON", T("verbose"), cs|BS_CHECKBOX, 0, 16, 336, 430, 22, g.hwnd, IDC_VERBOSE)
	setChecked(g.hVerbose, cfg.Verbose)

	g.hStatusLbl = createControl("STATIC", T("status"), cs|SS_LEFT, 0, 16, 362, 70, 20, g.hwnd, IDC_STATUS_LABEL)
	g.hStatus = createControl("STATIC", T("stopped"), cs|SS_LEFT, 0, 90, 362, 360, 20, g.hwnd, IDC_STATUS)
	g.hFGLbl = createControl("STATIC", T("foreground"), cs|SS_LEFT, 0, 16, 384, 70, 20, g.hwnd, IDC_FG_LABEL)
	g.hFG = createControl("STATIC", T("none"), cs|SS_LEFT, 0, 90, 384, 360, 20, g.hwnd, IDC_FG)

	g.hStart = createControl("BUTTON", T("start"), cs|BS_PUSHBUTTON, 0, 16, 414, 130, 32, g.hwnd, IDC_START)
	g.hSave = createControl("BUTTON", T("save"), cs|BS_PUSHBUTTON, 0, 170, 414, 130, 32, g.hwnd, IDC_SAVE)
	g.hQuit = createControl("BUTTON", T("quit"), cs|BS_PUSHBUTTON, 0, 324, 414, 130, 32, g.hwnd, IDC_QUIT)

	g.hLogLabel = createControl("STATIC", T("log_title"), cs|SS_LEFT, 0, 16, 454, 200, 18, g.hwnd, IDC_LOG_LABEL)
	g.hLog = createControl("EDIT", "",
		cs|WS_BORDER|ES_MULTILINE|ES_AUTOVSCROLL|WS_VSCROLL|ES_READONLY, edge,
		16, 474, 430, 54, g.hwnd, IDC_LOG)

	pSetTimer.Call(g.hwnd, 1, 500, 0)
}

// applyLang 用当前语言刷新所有控件文案。
func applyLang() {
	setText(g.hwnd, T("app_title"))
	setText(g.hLangLabel, T("lang"))
	setText(g.hSettings, T("settings"))
	setText(g.hKeysLabel, T("keys"))
	setText(g.hDigits, T("keys_digits"))
	setText(g.hLetters, T("keys_letters"))
	setText(g.hSpecial, T("keys_special"))
	setText(g.hKeysHint, T("keys_hint"))
	setText(g.hHoldLabel, T("hold"))
	setText(g.hStatusLbl, T("status"))
	setText(g.hFGLbl, T("foreground"))
	setText(g.hLogLabel, T("log_title"))
	setText(g.hReinject, T("reinject"))
	setText(g.hScancode, T("scancode"))
	setText(g.hDiag, T("diagnostic"))
	setText(g.hAutostart, T("autostart"))
	setText(g.hVerbose, T("verbose"))
	updateStartLabel()
	setText(g.hSave, T("save"))
	setText(g.hQuit, T("quit"))
	updateStatus()
}

func updateStartLabel() {
	if running {
		setText(g.hStart, T("stop"))
	} else {
		setText(g.hStart, T("start"))
	}
}

func updateStatus() {
	if running {
		if cfg.Diagnostic {
			setText(g.hStatus, T("diag_on"))
		} else {
			setText(g.hStatus, T("running"))
		}
	} else {
		setText(g.hStatus, T("stopped"))
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
	case WM_TIMER:
		if wParam == 1 {
			setText(g.hFG, foregroundTitle())
		}
		return 0
	case WM_CLOSE:
		if mb(0, T("confirm_quit"), T("app_title"), MB_YESNO|MB_ICONQUESTION) == IDYES {
			stopBridge()
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case WM_DESTROY:
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
