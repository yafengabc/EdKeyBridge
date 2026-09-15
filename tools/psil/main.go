//go:build windows

// Command psil lists the running processes together with their integrity
// level (IL) — i.e. the "privilege level" that decides whether UIPI silently
// drops input sent to a higher-privileged window.
//
//	go run ./tools/psil
//
// Processes you cannot open (system processes, protected processes, other
// sessions) show "n/a" for the IL column. Run as administrator to see more.
package main

import (
	"fmt"
	"sort"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
)

var (
	pCreateToolhelp32Snapshot  = kernel32.NewProc("CreateToolhelp32Snapshot")
	pProcess32FirstW           = kernel32.NewProc("Process32FirstW")
	pProcess32NextW            = kernel32.NewProc("Process32NextW")
	pOpenProcess               = kernel32.NewProc("OpenProcess")
	pCloseHandle               = kernel32.NewProc("CloseHandle")
	pOpenProcessToken          = advapi32.NewProc("OpenProcessToken")
	pGetTokenInformation       = advapi32.NewProc("GetTokenInformation")
	pGetSidSubAuthorityCount   = advapi32.NewProc("GetSidSubAuthorityCount")
	pGetSidSubAuthority        = advapi32.NewProc("GetSidSubAuthority")
)

const (
	TH32CS_SNAPPROCESS                = 0x00000002
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TOKEN_QUERY                       = 0x0008
	TokenIntegrityLevel               = 25
)

// SECURITY_MANDATORY_*_RID
const (
	ilLow       = 0x1000
	ilMedium    = 0x2000
	ilHigh      = 0x3000
	ilSystem    = 0x4000
	ilProtected = 0x5000
)

// processEntry32 mirrors the Win32 PROCESSENTRY32W struct.
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

func tokenIL(token uintptr) (uint32, bool) {
	var need uint32
	pGetTokenInformation.Call(token, TokenIntegrityLevel, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 64 // 第一次只取长度失败时按常见大小分配
	}
	buf := make([]byte, need)
	r, _, _ := pGetTokenInformation.Call(token, TokenIntegrityLevel,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return 0, false
	}
	// TOKEN_MANDATORY_LABEL.Label.Sid 位于缓冲区起始处（PSID）。
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
	// IL RID 是最后一个子授权项。
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

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func main() {
	snap, _, _ := pCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
	if snap == ^uintptr(0) { // INVALID_HANDLE_VALUE
		fmt.Println("CreateToolhelp32Snapshot failed")
		return
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

	// 高权限在前，方便看清哪些进程可能挡住注入；同级按 PID 升序。
	sort.Slice(procs, func(i, j int) bool {
		if procs[i].il != procs[j].il {
			return ilRank(procs[i].il) > ilRank(procs[j].il)
		}
		return procs[i].pid < procs[j].pid
	})

	fmt.Printf("%-7s %-30s %-10s %s\n", "PID", "NAME", "INTEGRITY", "BASEPRI")
	fmt.Println("----------------------------------------------------------------------")
	for _, p := range procs {
		fmt.Printf("%-7d %-30s %-10s %d\n", p.pid, truncate(p.name, 30), p.il, p.basePri)
	}
	fmt.Printf("\n%d processes\n", len(procs))
}
