//go:build windows

// Command psil-gui shows the running processes together with their integrity
// level (IL) — i.e. the "privilege level" that decides whether UIPI silently
// drops input sent to a higher-privileged window. It is a small Win32 GUI
// (syscall only, no third-party widgets) on top of the same enumeration the
// console version used to print.
//
// Build & run (clean GUI, no console window):
//
//	go build -H windowsgui -o psil.exe ./tools/psil && ./psil.exe
//
// Or just for a quick look (a console host stays attached):
//
//	go run ./tools/psil
//
// Processes you cannot open (system processes, protected processes, other
// sessions) show "n/a" for the IL column. Run as administrator to see more.
// Row colour tells you at a glance who could block injected input:
// red = high, purple = system/protected, gray = low.
package main

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
)

var (
	pCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	pProcess32NextW           = kernel32.NewProc("Process32NextW")
	pOpenProcess              = kernel32.NewProc("OpenProcess")
	pCloseHandle              = kernel32.NewProc("CloseHandle")
	pGetModuleFileNameW       = kernel32.NewProc("GetModuleFileNameW")
	pOpenProcessToken         = advapi32.NewProc("OpenProcessToken")
	pGetTokenInformation      = advapi32.NewProc("GetTokenInformation")
	pGetSidSubAuthorityCount  = advapi32.NewProc("GetSidSubAuthorityCount")
	pGetSidSubAuthority       = advapi32.NewProc("GetSidSubAuthority")
	pGetCurrentProcess        = kernel32.NewProc("GetCurrentProcess")

	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pUpdateWindow     = user32.NewProc("UpdateWindow")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pSendMessageW     = user32.NewProc("SendMessageW")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pSetWindowPos     = user32.NewProc("SetWindowPos")
	pGetClientRect    = user32.NewProc("GetClientRect")
	pLoadCursorW      = user32.NewProc("LoadCursorW")
	pMessageBoxW      = user32.NewProc("MessageBoxW")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	pShellExecuteW        = shell32.NewProc("ShellExecuteW")
)

const (
	TH32CS_SNAPPROCESS                = 0x00000002
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TOKEN_QUERY                       = 0x0008
	TokenIntegrityLevel               = 25

	IDC_ARROW = 32512

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_EX_CLIENTEDGE    = 0x00000200

	SW_SHOWNORMAL = 5
	SW_SHOW       = 5

	WM_DESTROY = 0x0002
	WM_SIZE    = 0x0005
	WM_COMMAND = 0x0111
	WM_NOTIFY  = 0x004E
	WM_PAINT   = 0x000F
	WM_APP_REFRESH = 0x8001

	COLOR_BTNFACE = 15

	WC_LISTVIEW                  = "SysListView32"
	LVM_FIRST                    = 0x1000
	LVM_DELETEALLITEMS           = LVM_FIRST + 9
	LVM_INSERTCOLUMNW            = LVM_FIRST + 97
	LVM_INSERTITEMW              = LVM_FIRST + 77
	LVM_SETITEMW                 = LVM_FIRST + 76
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVS_REPORT                   = 0x0001
	LVS_SINGLESEL                = 0x0004
	LVS_SHOWSELALWAYS            = 0x0008
	LVIF_TEXT                    = 0x0001
	LVCF_FMT                     = 0x0001
	LVCF_WIDTH                   = 0x0002
	LVCF_TEXT                    = 0x0004
	LVCF_SUBITEM                 = 0x0008
	LVCF_ORDER                   = 0x0020
	LVCFMT_LEFT                  = 0x0000
	LVS_EX_FULLROWSELECT         = 0x0020
	LVS_EX_GRIDLINES             = 0x0001
	LVS_EX_DOUBLEBUFFER          = 0x00010000

	SWP_NOZORDER  = 0x0004
	SWP_NOACTIVATE = 0x0010

	STATUSCLASSNAME = "msctls_statusbar32"
	SB_SETTEXTW     = 0x040B // WM_USER + 11
	SBARS_SIZEGRIP  = 0x0100

	ICC_LISTVIEW_CLASSES = 0x0001
	ICC_BAR_CLASSES      = 0x0004

	NM_CUSTOMDRAW       = 0xFFFFFFEC
	CDDS_PREPAINT       = 0x00000001
	CDDS_ITEMPREPAINT   = 0x00010001
	CDRF_NOTIFYITEMDRAW = 0x00000020
	CDRF_NEWFONT        = 0x00000002

	LVN_FIRST       = 0xFFFFFF9C
	LVN_COLUMNCLICK = LVN_FIRST - 8 // 0xFFFFFF94：ListView 通知码从 LVN_FIRST 往下数

	IDC_LIST    = 100
	IDC_REFRESH = 101
	IDC_ADMIN   = 102
	IDC_HINT    = 103

	BS_PUSHBUTTON = 0x00000000
	SS_LEFT       = 0x00000000

	MB_OK        = 0x00000000
	MB_ICONERROR = 0x00000010
)

// SECURITY_MANDATORY_*_RID
const (
	ilLow       = 0x1000
	ilMedium    = 0x2000
	ilHigh      = 0x3000
	ilSystem    = 0x4000
	ilProtected = 0x5000
)

type processEntry32 struct {
	dwSize              uint32
	cntUsage            uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	cntThreads          uint32
	th32ParentProcessID uint32
	pcPriClassBase      int32
	dwFlags             uint32
	szExeFile           [syscall.MAX_PATH]uint16
}

type procInfo struct {
	pid     uint32
	name    string
	il      string
	basePri int32
}

// lvItem matches the 64-bit LVITEMW layout (88 bytes). The compile-time check
// below fails the build if the layout drifts.
type lvItem struct {
	mask       uint32
	iItem      int32
	iSubItem   int32
	state      uint32
	stateMask  uint32
	pszText    *uint16
	cchTextMax int32
	iImage     int32
	lParam     uintptr
	iIndent    int32
	iGroupId   int32
	cColumns   uint32
	puColumns  *uint32
	piColFmt   *int32
	iGroup     int32
}

var _ [88]struct{} = [unsafe.Sizeof(lvItem{})]struct{}{}

type lvColumn struct {
	mask       uint32
	fmt        int32
	cx         int32
	pszText    *uint16
	cchTextMax int32
	iSubItem   int32
	iImage     int32
	iOrder     int32
}

var _ [40]struct{} = [unsafe.Sizeof(lvColumn{})]struct{}{}

type initCommonControls struct {
	dwSize uint32
	dwICC  uint32
}

type wndClassEx struct {
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

type rect struct {
	left, top, right, bottom int32
}

// nmhdr matches the 64-bit NMHDR layout. Used only to read idFrom/code from a
// WM_NOTIFY lParam (offsets 8 / 16 — correct even though Go pads this struct to
// 24; we never write through it).
type nmhdr struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
}

// nmCustomDraw matches the 64-bit NMCUSTOMDRAW layout. It is FLATTENED (no nested
// nmhdr) so the field offsets line up with the real Win64 notification memory.
// On Win64 NMHDR is 24 bytes (hwndFrom 0, idFrom 8, code 16, +4 tail pad), so
// drawStage sits at 24 (NOT 20): hwndFrom 0, idFrom 8, code 16, [pad] 20,
// drawStage 24, hdc 32, rc 40, itemSpec 56, itemState 60, itemParam 64.
// (The earlier "flatten without the 4-byte pad" read drawStage from padding = 0,
// so CDDS_* stages never matched and row coloring silently did nothing.)
type nmCustomDraw struct {
	hwndFrom  uintptr
	idFrom    uintptr
	code      uint32
	_         [4]byte
	drawStage uint32
	hdc       uintptr
	rc        [4]int32
	itemSpec  uint32
	itemState uint32
	itemParam uintptr
}

// nmListCustomDraw matches the NMLVCUSTOMDRAW prefix we actually read/write
// (clrText / clrTextBk / iSubItem). Fields after iSubItem are left out: we
// never touch them, and reading this prefix of the larger Windows struct is safe.
type nmListCustomDraw struct {
	hwndFrom  uintptr
	idFrom    uintptr
	code      uint32
	_         [4]byte
	drawStage uint32
	hdc       uintptr
	rc        [4]int32
	itemSpec  uint32
	itemState uint32
	itemParam uintptr
	clrText   uint32
	clrTextBk uint32
	iSubItem  int32
}

// compile-time layout assertion for NMCUSTOMDRAW (Win64 = 72 bytes).
var _ [72]struct{} = [unsafe.Sizeof(nmCustomDraw{})]struct{}{}

// compile-time layout assertion for NMLVCUSTOMDRAW prefix (Win64 = 88 bytes).
var _ [88]struct{} = [unsafe.Sizeof(nmListCustomDraw{})]struct{}{}

// nmListView matches the prefix of NMLISTVIEW we need (iSubItem, the clicked
// column). Flattened like nmCustomDraw so offsets line up with the real Win64
// notification memory. NMHDR is 24 bytes (code @16 + 4 tail pad), so iItem is at
// 24 and iSubItem at 28 — NOT 24. A header click reports iItem = -1, so reading
// iSubItem from offset 24 would always yield -1 and fall through to the BASEPRI
// (default) sort for every column.
type nmListView struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
	_        [4]byte
	iItem    int32
	iSubItem int32
	uNewState uint32
	uOldState uint32
	uChanged  uint32
	ptX, ptY int32
	lParam   uintptr
}

// compile-time layout assertion for NMLISTVIEW (Win64 = 64 bytes). iSubItem
// must sit at offset 28; if this ever fails the struct drifted and a
// column-click would sort by the wrong field (the "abstract ordering" symptom).
var _ [64]struct{} = [unsafe.Sizeof(nmListView{})]struct{}{}

var (
	gHwnd, gHwndLV, gHwndStatus, gHwndHint, gHwndRefresh, gHwndAdmin uintptr
	gMu      sync.Mutex
	gRows    []procInfo
	gSortCol int = 0  // 默认按 PID 列（最直观的数字序）
	gSortDir int = 1  // 1 升序；-1 降序
)

func tokenIL(token uintptr) (uint32, bool) {
	var need uint32
	pGetTokenInformation.Call(token, TokenIntegrityLevel, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 64
	}
	buf := make([]byte, need)
	r, _, _ := pGetTokenInformation.Call(token, TokenIntegrityLevel,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return 0, false
	}
	sid := *(*uintptr)(unsafe.Pointer(&buf[0]))
	if sid == 0 {
		return 0, false
	}
	cntPtr, _, _ := pGetSidSubAuthorityCount.Call(sid)
	if cntPtr == 0 {
		return 0, false
	}
	cnt := *(*uint8)(unsafe.Pointer(cntPtr))
	if cnt == 0 {
		return 0, false
	}
	subPtr, _, _ := pGetSidSubAuthority.Call(sid, uintptr(cnt-1))
	if subPtr == 0 {
		return 0, false
	}
	return *(*uint32)(unsafe.Pointer(subPtr)), true
}

func processIL(pid uint32) (uint32, bool) {
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return 0, false
	}
	defer pCloseHandle.Call(h)
	var token uintptr
	r, _, _ := pOpenProcessToken.Call(h, TOKEN_QUERY, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return 0, false
	}
	defer pCloseHandle.Call(token)
	return tokenIL(token)
}

func selfIL() (uint32, bool) {
	proc, _, _ := pGetCurrentProcess.Call()
	var token uintptr
	r, _, _ := pOpenProcessToken.Call(proc, TOKEN_QUERY, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return 0, false
	}
	defer pCloseHandle.Call(token)
	return tokenIL(token)
}

func ilName(rid uint32) string {
	switch {
	case rid >= ilProtected:
		return "protected"
	case rid >= ilSystem:
		return "system"
	case rid >= ilHigh:
		return "high"
	case rid >= ilMedium:
		if rid > ilMedium {
			return "medium+"
		}
		return "medium"
	case rid >= ilLow:
		return "low"
	default:
		return "untrusted"
	}
}

func ilRank(s string) int {
	switch s {
	case "protected":
		return 5
	case "system":
		return 4
	case "high":
		return 3
	case "medium+":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}

// ilColor 返回该完整性级别对应的文字颜色（RGB，0xBBGGRR）。
func ilColor(s string) uint32 {
	switch s {
	case "protected", "system":
		return 0x800080 // 紫
	case "high":
		return 0x0000FF // 红
	case "medium+":
		return 0x800000 // 深蓝
	case "medium":
		return 0x000000 // 黑
	default:
		return 0x808080 // 灰
	}
}

func enumerate() []procInfo {
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == ^uintptr(0) {
		return nil
	}
	defer pCloseHandle.Call(snap)

	var pe processEntry32
	pe.dwSize = uint32(unsafe.Sizeof(pe))

	var procs []procInfo
	if r, _, _ := pProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe))); r != 0 {
		for {
			name := syscall.UTF16ToString(pe.szExeFile[:])
			il := "n/a"
			if rid, ok := processIL(pe.th32ProcessID); ok {
				il = ilName(rid)
			}
			procs = append(procs, procInfo{
				pid:     pe.th32ProcessID,
				name:    name,
				il:      il,
				basePri: pe.pcPriClassBase,
			})
			if r, _, _ := pProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe))); r == 0 {
				break
			}
		}
	}

	return procs
}

// applySort 按当前 gSortCol/gSortDir 对 gRows 排序。调用方需持有 gMu。
func applySort() {
	sort.SliceStable(gRows, func(i, j int) bool {
		// before 返回升序时 x 是否应在 y 之前。
		before := func(x, y procInfo) bool {
			switch gSortCol {
			case 0: // PID
				return x.pid < y.pid
			case 1: // NAME
				return strings.ToLower(x.name) < strings.ToLower(y.name)
			case 2: // INTEGRITY
				return ilRank(x.il) < ilRank(y.il)
			default: // BASEPRI
				return x.basePri < y.basePri
			}
		}
		if gSortDir < 0 {
			// 降序必须用严格反向比较，否则相等元素会同时返回 true，
			// 违反 sort 的严格弱序要求，可能产出非单调（"乱"）的序列。
			return before(gRows[j], gRows[i])
		}
		return before(gRows[i], gRows[j])
	})
}

// startEnum 在后台 goroutine 里枚举进程，完成后把结果写回 gRows 并给主窗口
// 投一个 WM_APP_REFRESH。这样即便某个进程句柄调用很慢，UI 线程也永远不被卡住。
func startEnum() {
	go func() {
		procs := enumerate()
		gMu.Lock()
		gRows = procs
		gMu.Unlock()
		if gHwnd != 0 {
			pPostMessageW.Call(gHwnd, WM_APP_REFRESH, 0, 0)
		}
	}()
}

// populateList 读取最新结果并填充 ListView。必须在 UI 线程调用。
func populateList() {
	gMu.Lock()
	applySort()
	rows := make([]procInfo, len(gRows))
	copy(rows, gRows)
	gMu.Unlock()

	pSendMessageW.Call(gHwndLV, LVM_DELETEALLITEMS, 0, 0)
	for i, p := range rows {
		var item lvItem
		item.mask = LVIF_TEXT
		item.iItem = int32(i)
		ps := utf16Str(itoa(int(p.pid)))
		item.pszText = &ps[0]
		pSendMessageW.Call(gHwndLV, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&item)))

		setSub := func(sub int32, text string) {
			var it lvItem
			it.mask = LVIF_TEXT
			it.iItem = int32(i)
			it.iSubItem = sub
			ts := utf16Str(text)
			it.pszText = &ts[0]
			pSendMessageW.Call(gHwndLV, LVM_SETITEMW, 0, uintptr(unsafe.Pointer(&it)))
		}
		setSub(1, p.name)
		setSub(2, p.il)
		setSub(3, itoa(int(p.basePri)))
	}
	setStatus()
}

func setStatus() {
	gMu.Lock()
	n := len(gRows)
	col := gSortCol
	dir := gSortDir
	gMu.Unlock()
	self, ok := selfIL()
	selfTxt := "?"
	if ok {
		selfTxt = ilName(self)
	}
	admin := ""
	if selfTxt == "high" || selfTxt == "system" || selfTxt == "protected" {
		admin = "（管理员）"
	}
	var colName string
	switch col {
	case 0:
		colName = "PID"
	case 1:
		colName = "名称"
	case 2:
		colName = "完整性"
	default:
		colName = "基础优先级"
	}
	dirName := "升序"
	if dir < 0 {
		dirName = "降序"
	}
	msg := utf16Str("共 " + itoa(n) + " 个进程 · 排序=" + colName + dirName +
		" · 本程序 IL=" + selfTxt + admin + " · 以管理员运行可见更多")
	pSendMessageW.Call(gHwndStatus, SB_SETTEXTW, 0, uintptr(unsafe.Pointer(&msg[0])))
}

func layout() {
	if gHwndLV == 0 {
		return
	}
	var rc rect
	pGetClientRect.Call(gHwnd, uintptr(unsafe.Pointer(&rc)))
	w := rc.right - rc.left
	h := rc.bottom - rc.top
	const gap = 8
	const sbH = 22
	const btnH = 24
	const hintH = 26

	lvY := int32(gap) + hintH + gap
	lvH := h - lvY - (btnH + gap + sbH)
	if lvH < 40 {
		lvH = 40
	}
	flags := uintptr(SWP_NOZORDER | SWP_NOACTIVATE)
	pSetWindowPos.Call(gHwndHint, 0, uintptr(gap), uintptr(gap), uintptr(w-2*gap), uintptr(hintH), flags)
	pSetWindowPos.Call(gHwndLV, 0, uintptr(gap), uintptr(lvY), uintptr(w-2*gap), uintptr(lvH), flags)
	by := h - sbH - btnH
	pSetWindowPos.Call(gHwndRefresh, 0, uintptr(gap), uintptr(by), 90, btnH, flags)
	pSetWindowPos.Call(gHwndAdmin, 0, uintptr(gap+90+gap), uintptr(by), 140, btnH, flags)
	if gHwndStatus != 0 {
		pSendMessageW.Call(gHwndStatus, WM_SIZE, 0, 0)
	}
}

func onCommand(id int32) {
	switch id {
	case IDC_REFRESH:
		startEnum()
	case IDC_ADMIN:
		exe := moduleFileName()
		if exe == "" {
			mb("无法获取自身路径", "错误")
			return
		}
		rs := utf16Str("runas")
		re := utf16Str(exe)
		re2 := utf16Str("")
		r, _, _ := pShellExecuteW.Call(0,
			uintptr(unsafe.Pointer(&rs[0])),
			uintptr(unsafe.Pointer(&re[0])),
			0, uintptr(unsafe.Pointer(&re2[0])),
			SW_SHOWNORMAL)
		if r <= 32 {
			mb("提权失败（返回码 "+itoa(int(r))+"）", "提示")
			return
		}
		pPostQuitMessage.Call(0)
	}
}

// onColumnClick 处理表头点击：同一列再次点击切换升/降序；切换到一个新列时按该列
// 合适的默认方向排序。这样每列都有自己独立的"排序方式"——不仅是比较器不同
// （数值/字母/等级），连默认方向也因列而异。
func onColumnClick(col int) {
	gMu.Lock()
	if gSortCol == col {
		gSortDir = -gSortDir
	} else {
		gSortCol = col
		gSortDir = defaultSortDir(col)
	}
	gMu.Unlock()
	populateList()
}

// defaultSortDir 返回每列首次点击时的默认方向：INTEGRITY 列降序（高 IL 进程在前，
// 因为你最关心谁会挡输入），PID/名称/基础优先级 升序。
func defaultSortDir(col int) int {
	if col == 2 { // INTEGRITY
		return -1
	}
	return 1
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		// 主窗口自己不画东西，交给 DefWindowProc 配对 BeginPaint/EndPaint，
		// 否则更新区域永远不被清空 → 持续重绘 → 卡死。
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	case WM_COMMAND:
		id := int32(uint16(wParam & 0xFFFF))
		onCommand(id)
		return 0
	case WM_NOTIFY:
		nm := (*nmhdr)(unsafe.Pointer(lParam))
		if nm.idFrom == IDC_LIST {
			switch nm.code {
			case NM_CUSTOMDRAW:
				cd := (*nmListCustomDraw)(unsafe.Pointer(lParam))
				switch cd.drawStage {
				case CDDS_PREPAINT:
					return CDRF_NOTIFYITEMDRAW
				case CDDS_ITEMPREPAINT:
					idx := int(cd.itemSpec)
					gMu.Lock()
					ok := idx >= 0 && idx < len(gRows)
					il := ""
					if ok {
						il = gRows[idx].il
					}
					gMu.Unlock()
					if ok {
						cd.clrText = ilColor(il)
					}
					return CDRF_NEWFONT
				}
			case LVN_COLUMNCLICK:
				nl := (*nmListView)(unsafe.Pointer(lParam))
				onColumnClick(int(nl.iSubItem))
			}
		}
		return 0
	case WM_APP_REFRESH:
		populateList()
		return 0
	case WM_SIZE:
		layout()
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func moduleFileName() string {
	buf := make([]uint16, 260)
	pGetModuleFileNameW.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func utf16Str(s string) []uint16 {
	w := utf16.Encode([]rune(s))
	w = append(w, 0)
	return w
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func mb(text, title string) {
	ts := utf16Str(text)
	tl := utf16Str(title)
	pMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(&ts[0])),
		uintptr(unsafe.Pointer(&tl[0])),
		MB_OK|MB_ICONERROR)
}

func getModuleHandle() uintptr {
	p, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	return p
}

func main() {
	var icc initCommonControls
	icc.dwSize = uint32(unsafe.Sizeof(icc))
	icc.dwICC = ICC_LISTVIEW_CLASSES | ICC_BAR_CLASSES
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	hInstance := getModuleHandle()

	className := utf16Str("psilgui")
	staticClass := utf16Str("STATIC")
	listClass := utf16Str(WC_LISTVIEW)
	buttonClass := utf16Str("BUTTON")
	statusClass := utf16Str(STATUSCLASSNAME)
	hCursor, _, _ := pLoadCursorW.Call(0, uintptr(IDC_ARROW))

	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	wc.hInstance = hInstance
	wc.hCursor = hCursor
	wc.hbrBackground = uintptr(COLOR_BTNFACE + 1)
	wc.lpszClassName = &className[0]
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		mb("注册窗口类失败", "错误")
		return
	}

	title := utf16Str("进程完整性级别查看器 — Process Integrity Levels")
	gHwnd, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&className[0])),
		uintptr(unsafe.Pointer(&title[0])),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		0x80000000, 0x80000000,
		560, 620,
		0, 0, hInstance, 0,
	)
	if gHwnd == 0 {
		return // headless 沙箱：优雅退出
	}

	hintText := utf16Str("高 IL 进程（红）可通过 UIPI 拦截低 IL 程序的输入；以管理员运行可见更多。")
	gHwndHint, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&staticClass[0])),
		uintptr(unsafe.Pointer(&hintText[0])),
		WS_CHILD|WS_VISIBLE|SS_LEFT,
		8, 8, 540, 26,
		gHwnd, uintptr(IDC_HINT), hInstance, 0,
	)

	gHwndLV, _, _ = pCreateWindowExW.Call(
		WS_EX_CLIENTEDGE,
		uintptr(unsafe.Pointer(&listClass[0])),
		0,
		WS_CHILD|WS_VISIBLE|LVS_REPORT|LVS_SINGLESEL|LVS_SHOWSELALWAYS,
		8, 42, 540, 480,
		gHwnd, uintptr(IDC_LIST), hInstance, 0,
	)
	if gHwndLV == 0 {
		mb("创建列表失败", "错误")
		return
	}
	ex := LVS_EX_FULLROWSELECT | LVS_EX_GRIDLINES | LVS_EX_DOUBLEBUFFER
	pSendMessageW.Call(gHwndLV, LVM_SETEXTENDEDLISTVIEWSTYLE, uintptr(ex), uintptr(ex))

	cols := []struct {
		text string
		cx   int32
	}{
		{"PID", 70},
		{"NAME", 250},
		{"INTEGRITY", 120},
		{"BASEPRI", 70},
	}
	for i, c := range cols {
		var col lvColumn
		col.mask = LVCF_FMT | LVCF_WIDTH | LVCF_TEXT | LVCF_SUBITEM | LVCF_ORDER
		col.fmt = LVCFMT_LEFT
		col.cx = c.cx
		cs := utf16Str(c.text)
		col.pszText = &cs[0]
		col.iSubItem = int32(i)
		col.iOrder = int32(i)
		pSendMessageW.Call(gHwndLV, LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}

	refreshText := utf16Str("刷新")
	gHwndRefresh, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&buttonClass[0])),
		uintptr(unsafe.Pointer(&refreshText[0])),
		WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON,
		8, 540, 90, 24,
		gHwnd, uintptr(IDC_REFRESH), hInstance, 0,
	)
	adminText := utf16Str("以管理员运行")
	gHwndAdmin, _, _ = pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&buttonClass[0])),
		uintptr(unsafe.Pointer(&adminText[0])),
		WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON,
		106, 540, 140, 24,
		gHwnd, uintptr(IDC_ADMIN), hInstance, 0,
	)

	gHwndStatus, _, _ = pCreateWindowExW.Call(
		SBARS_SIZEGRIP,
		uintptr(unsafe.Pointer(&statusClass[0])),
		0,
		WS_CHILD|WS_VISIBLE,
		0, 0, 0, 0,
		gHwnd, 0, hInstance, 0,
	)

	layout()
	pShowWindow.Call(gHwnd, SW_SHOW)
	pUpdateWindow.Call(gHwnd)

	// 窗口显示后再去枚举，且放到后台线程，UI 线程立即进消息循环、全程可响应。
	startEnum()

	// 消息循环必须锁在创建窗口的这条 OS 线程上（syscall.NewCallback 的要求，
	// 也是 EdKeyBridge 验证过不卡死的关键）。
	runtime.LockOSThread()
	var m struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      struct {
			x, y int32
		}
	}
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
