package main

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

var (
	k32   = syscall.NewLazyDLL("kernel32.dll")
	adv   = syscall.NewLazyDLL("advapi32.dll")
	u32   = syscall.NewLazyDLL("user32.dll")
	psapi = syscall.NewLazyDLL("psapi.dll")

	pGetCurrentPID = k32.NewProc("GetCurrentProcessId")
	pOpenProcess   = k32.NewProc("OpenProcess")
	pCloseHandle   = k32.NewProc("CloseHandle")
	pOpenProcToken = adv.NewProc("OpenProcessToken")
	pGetTokenInfo  = adv.NewProc("GetTokenInformation")
	pSidSubCount   = adv.NewProc("GetSidSubAuthorityCount")
	pSidSubAuth    = adv.NewProc("GetSidSubAuthority")
	pRegGetValue   = adv.NewProc("RegGetValueW")
	pCreateSnap    = k32.NewProc("CreateToolhelp32Snapshot")
	pProc32First   = k32.NewProc("Process32FirstW")
	pProc32Next    = k32.NewProc("Process32NextW")
	pGetForeground = u32.NewProc("GetForegroundWindow")
	pGetWinPID     = u32.NewProc("GetWindowThreadProcessId")
	_              = psapi
)

const (
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TOKEN_QUERY                       = 0x0008
	TokenElevationType                = 18
	TokenElevation                    = 20
	TokenIntegrityLevel               = 25
	TH32CS_SNAPPROCESS                = 0x00000002
	MAX_PATH                          = 260
	HKEY_LOCAL_MACHINE                = 0x80000002
	RRF_RT_DWORD                      = 0x00000018
	ERROR_FILE_NOT_FOUND              = 2
)

type procEntry struct {
	Size          uint32
	CntUsage      uint32
	ProcessID     uint32
	_             [4]byte
	DefaultHeapID uintptr
	ModuleID      uint32
	CntThreads    uint32
	ParentProcID  uint32
	PriClassBase  int32
	Flags         uint32
	ExeFile       [MAX_PATH]uint16
}

func ilName(v uint32) string {
	switch v {
	case 0x0000:
		return "untrusted(0)"
	case 0x1000:
		return "LOW(低)"
	case 0x2000:
		return "MEDIUM(标准)"
	case 0x2100:
		return "MEDIUM+"
	case 0x3000:
		return "HIGH(管理员)"
	case 0x4000:
		return "SYSTEM"
	default:
		return fmt.Sprintf("0x%04X", v)
	}
}

// ilOf 返回指定 PID 的完整性级别 RID。
func ilOf(pid uint32) (uint32, error) {
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return 0, fmt.Errorf("open")
	}
	defer pCloseHandle.Call(h)

	var tok uintptr
	if r, _, _ := pOpenProcToken.Call(h, TOKEN_QUERY, uintptr(unsafe.Pointer(&tok))); r == 0 {
		return 0, fmt.Errorf("token")
	}
	defer pCloseHandle.Call(tok)
	return ilFromToken(tok)
}

func ilFromToken(tok uintptr) (uint32, error) {
	var need uint32
	pGetTokenInfo.Call(tok, TokenIntegrityLevel, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need < 16 || need > 4096 {
		need = 64
	}
	buf := make([]byte, need)
	r, _, _ := pGetTokenInfo.Call(tok, TokenIntegrityLevel,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return 0, fmt.Errorf("info")
	}
	// TOKEN_MANDATORY_LABEL { SID_AND_ATTRIBUTES Label }，Label.Sid 在偏移 0
	sid := *((*uintptr)(unsafe.Pointer(&buf[0])))
	if sid == 0 {
		return 0, fmt.Errorf("nil sid")
	}
	cntPtr, _, _ := pSidSubCount.Call(sid)
	if cntPtr == 0 {
		return 0, fmt.Errorf("nosub")
	}
	cnt := uintptr(*((*byte)(unsafe.Pointer(cntPtr)))) // 返回的是 PUCHAR, 必须解引用
	if cnt == 0 {
		return 0, fmt.Errorf("nosub")
	}
	p, _, _ := pSidSubAuth.Call(sid, cnt-1)
	if p == 0 {
		return 0, fmt.Errorf("nilsub")
	}
	return *((*uint32)(unsafe.Pointer(p))), nil
}

func regDWORD(subkey, value string) (uint32, error) {
	sk, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return 0, err
	}
	vn, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return 0, err
	}
	var out, sz uint32
	sz = 4
	r, _, _ := pRegGetValue.Call(
		uintptr(HKEY_LOCAL_MACHINE),
		uintptr(unsafe.Pointer(sk)),
		uintptr(unsafe.Pointer(vn)),
		uintptr(RRF_RT_DWORD),
		0,
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&sz)),
	)
	if r == ERROR_FILE_NOT_FOUND {
		return 0, fmt.Errorf("(默认)")
	}
	if r != 0 {
		return 0, fmt.Errorf("err=%d", r)
	}
	return out, nil
}

func uacReport() {
	const base = `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`
	fmt.Println("\n===== UAC 策略 (HKLM\\...\\Policies\\System) =====")

	if v, err := regDWORD(base, "EnableLUA"); err == nil {
		state := "关闭（不做令牌拆分）"
		if v != 0 {
			state = "开启（管理员账户被拆分为标准令牌）"
		}
		fmt.Printf("  EnableLUA                = %d  → %s\n", v, state)
	} else {
		fmt.Printf("  EnableLUA                = %v\n", err)
	}

	if v, err := regDWORD(base, "ConsentPromptBehaviorAdmin"); err == nil {
		desc := map[uint32]string{
			0: "0 = 从不通知（管理员直接静默提权，进程直接拿到 HIGH IL）",
			1: "1 = 凭据提示",
			2: "2 = 仅安全桌面同意提示（默认）",
			3: "3 = 同意提示",
			4: "4 = 凭据+同意",
			5: "5 = 默认同意提示",
		}[v]
		fmt.Printf("  ConsentPromptBehaviorAdmin = %d  → %s\n", v, desc)
	}

	if v, err := regDWORD(base, "PromptOnSecureDesktop"); err == nil {
		fmt.Printf("  PromptOnSecureDesktop    = %d\n", v)
	}
	if v, err := regDWORD(base, "EnableInstallerDetection"); err == nil {
		fmt.Printf("  EnableInstallerDetection = %d  (安装程序自动提权)\n", v)
	}
}

func snap(selfPID uint32) {
	var pe procEntry
	pe.Size = uint32(unsafe.Sizeof(pe))
	s, _, _ := pCreateSnap.Call(TH32CS_SNAPPROCESS, 0)
	if s == uintptr(^uintptr(0)) {
		fmt.Println("snapshot failed")
		return
	}
	defer pCloseHandle.Call(s)

	type row struct {
		pid uint32
		il  uint32
		exe string
	}
	var hi, interesting []row
	keywords := []string{"elite", "edlaunch", "steam", "epic", "frontier", "rustdesk", "edkeybridge", "change_key"}

	ok, _, _ := pProc32First.Call(s, uintptr(unsafe.Pointer(&pe)))
	for ok != 0 {
		exe := syscall.UTF16ToString(pe.ExeFile[:])
		pid := pe.ProcessID
		if il, err := ilOf(pid); err == nil {
			r := row{pid, il, exe}
			if il >= 0x3000 {
				hi = append(hi, r)
			}
			low := strings.ToLower(exe)
			for _, k := range keywords {
				if strings.Contains(low, k) {
					interesting = append(interesting, r)
					break
				}
			}
		}
		pe.ProcessID = 0
		pe.Size = uint32(unsafe.Sizeof(pe))
		ok, _, _ = pProc32Next.Call(s, uintptr(unsafe.Pointer(&pe)))
	}

	sort.Slice(hi, func(i, j int) bool { return hi[i].il > hi[j].il || (hi[i].il == hi[j].il && hi[i].exe < hi[j].exe) })
	fmt.Printf("\n=== 高完整性级别进程 (IL >= HIGH) 共 %d 个 ===\n", len(hi))
	for _, r := range hi {
		tag := ""
		if r.pid == selfPID {
			tag = "   <== 本程序"
		}
		fmt.Printf("  PID %-6d %-12s %s%s\n", r.pid, ilName(r.il), r.exe, tag)
	}
	fmt.Println("\n=== 关注进程 (ED / 启动器 / RustDesk) ===")
	if len(interesting) == 0 {
		fmt.Println("  (未发现相关进程 —— 若 ED 没在运行, 请启动 ED 后再跑一次)")
	}
	for _, r := range interesting {
		fmt.Printf("  PID %-6d %-12s %s\n", r.pid, ilName(r.il), r.exe)
	}
}

func main() {
	selfPID, _, _ := pGetCurrentPID.Call()
	fmt.Println("===== 完整性级别(IL) 诊断 =====")
	il, err := ilOf(uint32(selfPID))
	if err != nil {
		fmt.Println("取自身 IL 失败:", err)
	} else {
		fmt.Printf("本进程 PID=%d  IL = %s\n", selfPID, ilName(il))
	}

	// 令牌类型
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(selfPID))
	var tok uintptr
	if r, _, _ := pOpenProcToken.Call(h, TOKEN_QUERY, uintptr(unsafe.Pointer(&tok))); r != 0 {
		var et, need uint32
		pGetTokenInfo.Call(tok, TokenElevationType, uintptr(unsafe.Pointer(&et)), 4, uintptr(unsafe.Pointer(&need)))
		switch et {
		case 1:
			fmt.Println("  令牌类型: Default (未拆分 — UAC 关闭, 或本身就是完整令牌)")
		case 2:
			fmt.Println("  令牌类型: Full (已提权, 拿到完整管理员令牌)")
		case 3:
			fmt.Println("  令牌类型: Limited (已拆分 — 账户是管理员, 但当前只有标准令牌)")
		}
		var el, n2 uint32
		pGetTokenInfo.Call(tok, TokenElevation, uintptr(unsafe.Pointer(&el)), 4, uintptr(unsafe.Pointer(&n2)))
		fmt.Printf("  已提权: %v\n", el != 0)
		pCloseHandle.Call(tok)
	}
	pCloseHandle.Call(h)

	if hwnd, _, _ := pGetForeground.Call(); hwnd != 0 {
		var pid uint32
		pGetWinPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if fil, err := ilOf(pid); err == nil {
			fmt.Printf("前台窗口进程 PID=%d  IL = %s\n", pid, ilName(fil))
		}
	}

	uacReport()
	snap(uint32(selfPID))
}
