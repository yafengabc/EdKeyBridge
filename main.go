//go:build windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runSelfTest 在编译后的二进制内跑一遍配置与语言逻辑，结果写入日志文件后退出。
// 用于无桌面环境下确认程序可正常启动并执行核心逻辑。
func runSelfTest() {
	var b strings.Builder
	ok := true

	s := Settings{Lang: "auto", KeysDig: true, KeysLet: true, KeysSpec: false,
		HoldMs: 120, Reinject: true, Scancode: false, Diagnostic: true,
		Autostart: false, Verbose: true}
	tmp := "edkeybridge_selftest.toml"
	_ = os.Remove(tmp)
	if err := saveSettings(tmp, s); err != nil {
		ok = false
		b.WriteString("SAVE FAIL: " + err.Error() + "\n")
	}
	loaded := loadSettings(tmp)
	_ = os.Remove(tmp)
	if !loaded.KeysDig || !loaded.KeysLet || loaded.KeysSpec ||
		loaded.HoldMs != 120 || !loaded.Reinject ||
		!loaded.Diagnostic || loaded.Autostart ||
		!loaded.Verbose || loaded.Lang != "auto" {
		ok = false
		b.WriteString(fmt.Sprintf("ROUNDTRIP FAIL: %+v\n", loaded))
	} else {
		b.WriteString("TOML roundtrip OK\n")
	}

	sys := detectSystemLang()
	b.WriteString("system lang = " + sys + "\n")
	if resolveLang("auto") != sys {
		ok = false
		b.WriteString("resolveLang(auto) mismatch\n")
	}
	if resolveLang("zh") != "zh" || resolveLang("en") != "en" {
		ok = false
		b.WriteString("resolveLang zh/en mismatch\n")
	} else {
		b.WriteString("lang resolve OK\n")
	}

	curLang = resolveLang(cfg.Lang)
	b.WriteString(traySelfTest() + "\n")
	b.WriteString("T(start)=" + T("start") + "\n")
	b.WriteString("T(quit)=" + T("quit") + "\n")

	if ok {
		b.WriteString("SELFTEST PASS\n")
	} else {
		b.WriteString("SELFTEST FAIL\n")
	}
	logPath := "edkeybridge_selftest.log"
	if exe, err := os.Executable(); err == nil {
		logPath = filepath.Join(filepath.Dir(exe), "edkeybridge_selftest.log")
	}
	_ = os.WriteFile(logPath, []byte(b.String()), 0644)
	if !ok {
		os.Exit(1)
	}
}

func main() {
	selftest := flag.Bool("selftest", false, "运行自检并退出")
	// -resume：由提权重启（UAC）拉起时使用。强制"启动即开始桥接"，
	// 不受配置文件里 autostart 的影响 —— 提权的目的就是让桥接着上，不该停在"已停止"。
	resume := flag.Bool("resume", false, "提权重启后自动恢复桥接")
	flag.Parse()
	if *selftest {
		runSelfTest()
		return
	}

	// 键盘钩子必须在具有消息循环的线程上安装，因此锁定当前 goroutine 到 OS 线程。
	runtime.LockOSThread()

	cfg = loadSettings(configPath())
	if *resume {
		cfg.Autostart = true
	}
	curLang = resolveLang(cfg.Lang)
	log.Println("EdKeyBridge " + versionInfo())

	if !createGUI() {
		return
	}

	log.SetFlags(0)
	log.SetOutput(guiLogWriter{hwnd: g.hLog})

	setLangCombo()
	applyLang()
	addTrayIcon()
	log.Println(fmt.Sprintf(T("cfg_path"), configPath()))

	if !isAdmin() {
		log.Println(T("admin_warn"))
	}

	if cfg.Autostart {
		if err := startBridge(); err != nil {
			log.Println(err.Error())
		} else if *resume {
			log.Println(T("elevate_resumed"))
			flashTaskbar(g.hwnd, true)
			// 旧实例退出前会把它临时最小化的游戏还原回去，那一下会把焦点抢走。
			// 所以延后一点再把本程序抬到前台，否则刚抬起来又被游戏盖住。
			pSetTimer.Call(g.hwnd, timerBringToFront, 900, 0)
		}
	}
	updateStartLabel()
	updateStatus()

	runGUI()

	if running {
		stopBridge()
	}
	if hookHandle != 0 {
		pUnhookWindowsHookEx.Call(hookHandle)
	}
}
