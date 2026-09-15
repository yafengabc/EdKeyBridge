//go:build windows

package main

// 完整性级别（Integrity Level, IL）检测。
//
// Windows 的 UIPI（用户界面特权隔离）会静默丢弃低 IL 进程发往高 IL 进程的
// 合成输入（SendInput）。当目标游戏以管理员权限运行、而本程序不是时，注入的
// 按键会被系统丢弃——表现为“记事本能收到、游戏完全没反应”。
// 本文件用于把这种不可见的情况显式暴露出来。

import (
	"syscall"
	"unsafe"
)

const (
	TOKEN_QUERY                       = 0x0008
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TokenIntegrityLevel               = 25
)

// SECURITY_MANDATORY_*_RID
const (
	ilUntrusted  = 0x00000000
	ilLow        = 0x00001000
	ilMedium     = 0x00002000
	ilHigh       = 0x00003000
	ilSystem     = 0x00004000
	ilProtected  = 0x00005000
)

var advapi32 = syscall.NewLazyDLL("advapi32.dll")

var (
	pOpenProcessToken        = advapi32.NewProc("OpenProcessToken")
	pGetTokenInformation     = advapi32.NewProc("GetTokenInformation")
	pGetSidSubAuthorityCount = advapi32.NewProc("GetSidSubAuthorityCount")
	pGetSidSubAuthority      = advapi32.NewProc("GetSidSubAuthority")

	// kernel32 / user32 的 LazyDLL 已在 bridge.go 中声明，此处仅补新 Proc。
	pOpenProcess              = kernel32.NewProc("OpenProcess")
	pCloseHandle              = kernel32.NewProc("CloseHandle")
	pGetCurrentProcess        = kernel32.NewProc("GetCurrentProcess")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

// tokenIL 从访问令牌中读出完整性级别 RID。
func tokenIL(token uintptr) (uint32, bool) {
	var need uint32
	pGetTokenInformation.Call(token, TokenIntegrityLevel, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 64 // 兜底：先取长度失败时按常见大小分配
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

func foregroundPID() uint32 {
	hwnd, _, _ := pGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
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

// ilReport 返回本程序与前台进程的 IL 名称，并报告是否因权限不足导致输入被丢弃。
// blocked == true 表示：前台进程 IL 高于本程序，SendInput 会被 UIPI 丢弃。
func ilReport() (selfName, foreName string, blocked bool) {
	s, okS := selfIL()
	if !okS {
		return "?", "?", false
	}
	selfName = ilName(s)

	pid := foregroundPID()
	if pid == 0 {
		return selfName, "?", false
	}
	f, okF := processIL(pid)
	if !okF {
		return selfName, "?", false
	}
	foreName = ilName(f)
	blocked = f > s
	return selfName, foreName, blocked
}
