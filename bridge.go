//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	WH_KEYBOARD_LL = 13
	HC_ACTION      = 0

	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_SYSKEYDOWN = 0x0104
	WM_SYSKEYUP   = 0x0105

	VK_PACKET = 0xE7

	LLKHF_LOWER_IL_INJECTED = 0x02
	LLKHF_INJECTED          = 0x10

	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002
	KEYEVENTF_SCANCODE    = 0x0008

	INPUT_KEYBOARD  = 1
	MAPVK_VK_TO_VSC = 0

	MAGIC_SELF = 0x00EDB10C

	VK_SHIFT = 0x10
)

type kbdllhookstruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type keybdinput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type input struct {
	typ    uint32
	pad    uint32
	ki     keybdinput
	unused [8]byte
}

func init() {
	if unsafe.Sizeof(input{}) != 40 {
		panic(fmt.Sprintf("INPUT size wrong: %d", unsafe.Sizeof(input{})))
	}
}

type winMSG struct {
	hwnd     uintptr
	message  uint32
	_        uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX      int32
	ptY      int32
	lPrivate uint32
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32.NewProc("CallNextHookEx")
	pGetMessage          = user32.NewProc("GetMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pSendInput           = user32.NewProc("SendInput")
	pVkKeyScan           = user32.NewProc("VkKeyScanW")
	pMapVirtualKey       = user32.NewProc("MapVirtualKeyW")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pGetWindowText       = user32.NewProc("GetWindowTextW")

	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	pIsUserAnAdmin   = shell32.NewProc("IsUserAnAdmin")
)

var (
	hookHandle uintptr
	running    bool

	br = newBridge()
)

func isAdmin() bool {
	ret, _, _ := pIsUserAnAdmin.Call()
	return ret != 0
}

func foregroundTitle() string {
	hwnd, _, _ := pGetForegroundWindow.Call()
	if hwnd == 0 {
		return T("none")
	}
	buf := make([]uint16, 256)
	n, _, _ := pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return T("none")
	}
	s := strings.TrimSpace(syscall.UTF16ToString(buf[:n]))
	if s == "" {
		return T("none")
	}
	if r := []rune(s); len(r) > 32 {
		s = string(r[:32]) + "…"
	}
	return s
}

func vkToScan(vk uint16) uint16 {
	s, _, _ := pMapVirtualKey.Call(uintptr(vk), MAPVK_VK_TO_VSC)
	if s == 0 {
		return vk
	}
	return uint16(s)
}

var vkNames = map[uint16]string{
	0x08: "Bksp", 0x09: "Tab", 0x0D: "Enter", 0x1B: "Esc", 0x20: "Space",
	0x10: "Shift", 0x11: "Ctrl", 0x12: "Alt",
	0x21: "PgUp", 0x22: "PgDn", 0x23: "End", 0x24: "Home",
	0x25: "←", 0x26: "↑", 0x27: "→", 0x28: "↓",
	0x2D: "Ins", 0x2E: "Del", 0x5B: "LWin", 0x5C: "RWin",
}

func vkName(vk uint16) string {
	if s, ok := vkNames[vk]; ok {
		return s
	}
	if vk >= 0x70 && vk <= 0x87 {
		return fmt.Sprintf("F%d", vk-0x6F)
	}
	return fmt.Sprintf("vk=0x%02X", vk)
}

func isExtendedVK(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24,
		0x25, 0x26, 0x27, 0x28,
		0x2D, 0x2E,
		0x5B, 0x5C,
		0xA3, 0xA5:
		return true
	}
	return false
}

func sendKey(vk, sc uint16, down bool) bool {
	var in input
	in.typ = INPUT_KEYBOARD
	if cfg.Scancode {
		in.ki.wScan = sc
		in.ki.dwFlags = KEYEVENTF_SCANCODE
	} else {
		in.ki.wVk = vk
		in.ki.wScan = sc
	}
	if isExtendedVK(vk) {
		in.ki.dwFlags |= KEYEVENTF_EXTENDEDKEY
	}
	if !down {
		in.ki.dwFlags |= KEYEVENTF_KEYUP
	}
	in.ki.dwExtraInfo = MAGIC_SELF

	r, _, err := pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r != 1 {
		log.Printf("SendInput 失败: %v", err)
		return false
	}
	return true
}

var fixedVK = map[rune]uint16{
	' ': 0x20, '-': 0xBD, '=': 0xBB, '[': 0xDB, ']': 0xDD, '\\': 0xDC,
	';': 0xBA, '\'': 0xDE, ',': 0xBC, '.': 0xBE, '/': 0xBF, '`': 0xC0,
}

func init() {
	for i := '0'; i <= '9'; i++ {
		fixedVK[i] = uint16(i)
	}
	for i := 'A'; i <= 'Z'; i++ {
		fixedVK[i] = uint16(i)
	}
	for i := 'a'; i <= 'z'; i++ {
		fixedVK[i] = uint16(i - 'a' + 'A')
	}
}

func charToVK(ch rune) (vk, sc uint16, mods uint8, ok bool) {
	if ch > 0xFFFF || (ch >= 0xD800 && ch <= 0xDFFF) {
		return 0, 0, 0, false
	}
	if r, _, _ := pVkKeyScan.Call(uintptr(uint16(ch))); int16(r) != -1 {
		vk = uint16(uint8(r & 0xFF))
		mods = uint8((r >> 8) & 0xFF)
	}
	if vk == 0 {
		if v, found := fixedVK[ch]; found {
			vk = v
			if ch >= 'A' && ch <= 'Z' {
				mods |= 0x01
			}
		}
	}
	if vk == 0 {
		return 0, 0, 0, false
	}
	return vk, vkToScan(vk), mods, true
}

type keyEvt struct {
	ch   rune
	vk   uint16
	sc   uint16
	down bool
}

type keyState struct {
	mu      sync.Mutex
	vk, sc  uint16
	mods    uint8
	label   string
	pressed bool
	downAt  time.Time
}

type bridge struct {
	evCh    chan keyEvt
	states  sync.Map
	mapCache sync.Map
	digits  bool
	letters bool
	special bool
}

func newBridge() *bridge {
	return &bridge{evCh: make(chan keyEvt, 256)}
}

func (b *bridge) initKeys() {
	b.digits = cfg.KeysDig
	b.letters = cfg.KeysLet
	b.special = cfg.KeysSpec
}

// want 按字符类别判断是否转换：数字 / 字母 / 特殊按键（其余）。
func (b *bridge) want(ch rune) bool {
	switch {
	case ch >= '0' && ch <= '9':
		return b.digits
	case (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z'):
		return b.letters
	default:
		return b.special
	}
}

func (b *bridge) canMap(ch rune) bool {
	if v, ok := b.mapCache.Load(ch); ok {
		return v.(bool)
	}
	_, _, _, ok := charToVK(ch)
	b.mapCache.Store(ch, ok)
	return ok
}

func (b *bridge) post(e keyEvt) {
	select {
	case b.evCh <- e:
	default:
		log.Println("警告：事件队列已满，丢弃")
	}
}

func (b *bridge) stateOf(k any) *keyState {
	if v, ok := b.states.Load(k); ok {
		return v.(*keyState)
	}
	v, _ := b.states.LoadOrStore(k, &keyState{})
	return v.(*keyState)
}

func (b *bridge) loop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case e := <-b.evCh:
			b.handle(e)
		case <-t.C:
			b.sweep()
		}
	}
}

func (b *bridge) handle(e keyEvt) {
	var key any = e.ch
	if e.vk != 0 {
		key = fmt.Sprintf("vk:%d", e.vk)
	}
	st := b.stateOf(key)

	if !e.down {
		go b.release(st)
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.pressed {
		return
	}

	vk, sc, mods := e.vk, e.sc, uint8(0)
	if e.vk == 0 {
		var ok bool
		vk, sc, mods, ok = charToVK(e.ch)
		if !ok {
			log.Printf("  字符 %q 无法映射到虚拟键码，跳过", e.ch)
			return
		}
	}

	st.vk, st.sc, st.mods = vk, sc, mods
	st.label = keyLabel(e.ch, vk)
	st.pressed = true
	st.downAt = time.Now()

	if mods&0x01 != 0 {
		sendKey(VK_SHIFT, 0x2A, true)
	}
	ok := sendKey(vk, sc, true)
	if !ok {
		log.Printf("! 按下失败 %s", st.label)
	}
	if cfg.Verbose {
		log.Printf("  ↓ %s vk=0x%02X scan=0x%02X", st.label, vk, sc)
	}
}

func (b *bridge) release(st *keyState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.pressed {
		return
	}
	if el := time.Since(st.downAt); el < time.Duration(cfg.HoldMs)*time.Millisecond {
		time.Sleep(time.Duration(cfg.HoldMs)*time.Millisecond - el)
	}
	ok := sendKey(st.vk, st.sc, false)
	if st.mods&0x01 != 0 {
		sendKey(VK_SHIFT, 0x2A, false)
	}
	st.pressed = false
	if !ok {
		log.Printf("! 抬起失败 %s", st.label)
		return
	}
	log.Printf("%s %dms", st.label, time.Since(st.downAt).Milliseconds())
}

func keyLabel(ch rune, vk uint16) string {
	if ch != 0 {
		return fmt.Sprintf("%q", ch)
	}
	return vkName(vk)
}

func (b *bridge) sweep() {
	b.states.Range(func(_, v any) bool {
		st := v.(*keyState)
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.pressed && time.Since(st.downAt) > 30*time.Second {
			log.Printf("! 超时释放 %s", st.label)
			sendKey(st.vk, st.sc, false)
			st.pressed = false
		}
		return true
	})
}

func (b *bridge) releaseAll() {
	b.states.Range(func(_, v any) bool {
		st := v.(*keyState)
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.pressed {
			sendKey(st.vk, st.sc, false)
			st.pressed = false
		}
		return true
	})
}

func hookProc(code, wParam, lParam uintptr) uintptr {
	if code != HC_ACTION || lParam == 0 {
		return callNext(code, wParam, lParam)
	}

	kb := (*kbdllhookstruct)(unsafe.Pointer(lParam))
	vk, sc := kb.vkCode, kb.scanCode
	extra := uint64(kb.dwExtraInfo)

	down := wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN
	up := wParam == WM_KEYUP || wParam == WM_SYSKEYUP
	if !down && !up {
		return callNext(code, wParam, lParam)
	}

	if extra&0xFFFFFFFF == MAGIC_SELF {
		return callNext(code, wParam, lParam)
	}

	injected := kb.flags&LLKHF_INJECTED != 0 || kb.flags&LLKHF_LOWER_IL_INJECTED != 0

	if cfg.Verbose {
		chDesc := ""
		if vk == VK_PACKET {
			chDesc = fmt.Sprintf(" unicode=%q(0x%04X)", rune(sc&0xFFFF), sc&0xFFFF)
		}
		log.Printf("事件 vk=0x%02X scan=0x%04X extra=%d injected=%v %s%s [%s]",
			vk, sc, extra, injected, dirName(down), chDesc, foregroundTitle())
	}

	if cfg.Diagnostic {
		return callNext(code, wParam, lParam)
	}

	if vk == VK_PACKET {
		ch := rune(sc & 0xFFFF)
		switch {
		case !br.want(ch):
			verbose("放行：%q 不在转换集合内", ch)
			return callNext(code, wParam, lParam)
		case !br.canMap(ch):
			verbose("放行：%q 无法映射为物理键", ch)
			return callNext(code, wParam, lParam)
		}
		br.post(keyEvt{ch: ch, down: down})
		return 1
	}

	if cfg.Reinject && injected {
		br.post(keyEvt{vk: uint16(vk), sc: uint16(sc), down: down})
		return 1
	}

	return callNext(code, wParam, lParam)
}

func verbose(format string, a ...any) {
	if cfg.Verbose {
		log.Printf("  "+format, a...)
	}
}

func dirName(down bool) string {
	if down {
		return "DOWN"
	}
	return "UP  "
}

func callNext(code, wParam, lParam uintptr) uintptr {
	ret, _, _ := pCallNextHookEx.Call(hookHandle, code, wParam, lParam)
	return ret
}

func installHook() error {
	cb := syscall.NewCallback(func(code, wParam, lParam uintptr) uintptr {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("hook panic: %v", r)
			}
		}()
		return hookProc(code, wParam, lParam)
	})
	mod, _, _ := pGetModuleHandle.Call(0)
	h, _, err := pSetWindowsHookEx.Call(uintptr(WH_KEYBOARD_LL), cb, mod, 0)
	if h == 0 {
		return fmt.Errorf("安装 WH_KEYBOARD_LL 失败: %v", err)
	}
	hookHandle = h
	return nil
}

func runMessageLoop() {
	var msg winMSG
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			return
		}
	}
}

// startBridge 初始化按键集合、启动处理协程并安装键盘钩子。
func startBridge() error {
	if running {
		return nil
	}
	br.initKeys()
	go br.loop()
	if err := installHook(); err != nil {
		return err
	}
	running = true
	if cfg.Diagnostic {
		log.Println(T("diag_on"))
	} else {
		log.Println(T("started"))
	}
	return nil
}

// stopBridge 释放所有按下中的按键并卸载钩子。
func stopBridge() {
	if !running {
		return
	}
	br.releaseAll()
	if hookHandle != 0 {
		pUnhookWindowsHookEx.Call(hookHandle)
		hookHandle = 0
	}
	running = false
	log.Println(T("stopped_msg"))
}
