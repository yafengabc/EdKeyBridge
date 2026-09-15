package main

// 系统语言检测：通过 GetUserDefaultUILanguage 取 LANGID，
// 低 10 位为主语言；中文(primary=0x04)返回 zh，其余一律 en。
var pGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

// curLang 是当前生效的语言（"zh" / "en"），由 resolveLang 在启动时与切换时计算。
var curLang = "en"

// T 返回当前语言下的文案；缺省回退到 key 本身。
func T(id string) string {
	if pair, ok := i18n[id]; ok {
		if curLang == "zh" {
			return pair[0]
		}
		return pair[1]
	}
	return id
}

// detectSystemLang 检测系统 UI 主语言。
func detectSystemLang() string {
	r, _, _ := pGetUserDefaultUILanguage.Call()
	langid := uint16(r)
	primary := langid & 0x3FF
	if primary == 0x04 { // LANG_CHINESE
		return "zh"
	}
	return "en"
}

// resolveLang 把配置里的 lang 设置解析为实际语言。
// "auto"/空 -> 跟随系统；"zh" -> 中文；其余 -> 英文。
func resolveLang(setting string) string {
	switch setting {
	case "zh":
		return "zh"
	case "en":
		return "en"
	default: // "auto" 或任何未知值
		return detectSystemLang()
	}
}

// i18n 内置中英文案。每个条目为 [中文, 英文]。
var i18n = map[string][2]string{
	"app_title":    {"EdKeyBridge", "EdKeyBridge"},
	"lang":         {"语言", "Language"},
	"settings":     {"设置", "Settings"},
	"keys":         {"转换按键", "Keys to convert"},
	"keys_digits":  {"数字 (0-9)", "Digits (0-9)"},
	"keys_letters": {"字母 (A-Z)", "Letters (A-Z)"},
	"keys_special": {"特殊按键（空格/符号等）", "Special keys (space/symbols…)"},
	"keys_hint":    {"勾选需要转换的按键分组", "Tick the key groups to convert"},
	"hold":         {"最小按下时长 (ms)", "Min hold time (ms)"},
	"reinject":     {"重发注入的真实按键", "Re-inject injected keys"},
	"scancode":     {"用扫描码注入", "Inject via scan code"},
	"diagnostic":   {"诊断模式（仅观察，不修改）", "Diagnostic mode (observe only)"},
	"autostart":    {"启动时自动开始", "Auto-start on launch"},
	"verbose":      {"详细日志", "Verbose logging"},
	"status":       {"状态", "Status"},
	"running":      {"运行中", "Running"},
	"stopped":      {"已停止", "Stopped"},
	"foreground":   {"前台窗口", "Foreground"},
	"start":        {"开始", "Start"},
	"stop":         {"停止", "Stop"},
	"save":         {"保存配置", "Save"},
	"quit":         {"退出", "Quit"},
	"saved":        {"配置已保存", "Config saved"},
	"confirm_quit": {"确定退出？未释放的按键将被释放。", "Quit? Stuck keys will be released."},
	"admin_warn":   {"未以管理员运行，部分游戏可能不响应按键。", "Not running as admin; some games may ignore keys."},
	"started":      {"桥接已启动", "Bridge started"},
	"stopped_msg":  {"桥接已停止", "Bridge stopped"},
	"diag_on":      {"诊断模式：仅观察，不转换输入", "Diagnostic: observing only, not converting"},
	"log_title":    {"日志", "Log"},
	"none":         {"（无）", "(none)"},
}
