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
	"app_title":        {"EdKeyBridge", "EdKeyBridge"},
	"lang":             {"语言", "Language"},
	"settings":         {"设置", "Settings"},
	"keys":             {"转换按键", "Keys to convert"},
	"keys_digits":      {"数字 (0-9)", "Digits (0-9)"},
	"keys_letters":     {"字母 (A-Z)", "Letters (A-Z)"},
	"keys_special":     {"特殊按键", "Special keys"},
	"keys_hint":        {"勾选要转换的分组；特殊 = 空格与符号等", "Tick groups to convert; special = space/symbols"},
	"hold":             {"最小按下时长 (ms)", "Min hold time (ms)"},
	"reinject":         {"重发注入的真实按键", "Re-inject injected keys"},
	"scancode":         {"用扫描码注入", "Inject via scan code"},
	"diagnostic":       {"诊断模式（仅观察，不修改）", "Diagnostic mode (observe only)"},
	"autostart":        {"启动时自动开始", "Auto-start on launch"},
	"verbose":          {"详细日志", "Verbose logging"},
	"status":           {"状态", "Status"},
	"running":          {"运行中", "Running"},
	"stopped":          {"已停止", "Stopped"},
	"stat_diag":        {"诊断中", "Diag"},
	"foreground":       {"前台窗口", "Foreground"},
	"start":            {"开始", "Start"},
	"stop":             {"停止", "Stop"},
	"save":             {"保存配置", "Save"},
	"quit":             {"退出", "Quit"},
	"saved":            {"配置已保存", "Config saved"},
	"confirm_quit":     {"确定退出？未释放的按键将被释放。", "Quit? Stuck keys will be released."},
	"admin_warn":       {"未以管理员运行，部分游戏可能不响应按键。", "Not running as admin; some games may ignore keys."},
	"started":          {"桥接已启动", "Bridge started"},
	"stopped_msg":      {"桥接已停止", "Bridge stopped"},
	"diag_on":          {"诊断模式：仅观察，不转换输入", "Diagnostic: observing only, not converting"},
	"log_title":        {"日志", "Log"},
	"cfg_path":         {"配置文件: %s", "Config file: %s"},
	"tray_open":        {"打开窗口", "Open window"},
	"tray_minimized":   {"已最小化到托盘，点托盘图标即可恢复（若托盘区看不到图标，点任务栏的 ^ 展开）", "Minimized to tray; click the tray icon to restore (if it's not visible, expand the ^ overflow on the taskbar)"},
	"none":             {"（无）", "(none)"},
	"il_higher":        {"⚠ 权限更高", "⚠ higher privilege"},
	"il_blocked":       {"⚠ 前台进程权限(%s)高于本程序(%s)：注入的按键会被系统丢弃。请以管理员身份运行本程序，或让游戏不以管理员运行（若 Steam 以管理员启动，游戏会继承其权限）", "⚠ Foreground process outranks this app (%s vs %s): injected keys are dropped. Run this app as administrator, or run the game un-elevated (if Steam runs as admin the game inherits it)."},
	"elevate_ask":      {"前台窗口「%s」的权限(%s)高于本程序(%s)，注入的按键会被系统丢弃，游戏收不到操作。\n\n是否以管理员身份重启 EdKeyBridge？（会弹出 UAC 确认，重启后自动继续桥接）", "The foreground window \"%s\" (%s) outranks this app (%s), so injected keys are dropped and the game won't respond.\n\nRestart EdKeyBridge as administrator? (Windows will ask for confirmation; bridging resumes automatically.)"},
	"elevate_launch":   {"正在以管理员身份重启…", "Restarting as administrator…"},
	"elevate_resumed":  {"已由管理员实例接管，桥接自动继续。", "Elevated instance took over; bridging resumed automatically."},
	"elevate_failcode": {"提权失败，ShellExecute 返回码 %d（5=UAC 被取消或被策略阻止，2/3=exe 路径无效）", "Elevation failed, ShellExecute returned %d (5 = UAC cancelled/blocked, 2/3 = invalid exe path)"},
	"elevate_minimize": {"已临时最小化全屏窗口以显示提示，稍后会自动还原。", "Temporarily minimized the full-screen window so the prompt is visible; it will be restored."},
	"elevate_fail":     {"提权失败：UAC 被取消或受策略阻止。请手动右键 EdKeyBridge →「以管理员身份运行」，或让游戏不以管理员启动。", "Elevation failed: UAC was cancelled or blocked by policy. Right-click EdKeyBridge → Run as administrator, or start the game un-elevated."},
	"elevate_declined": {"已跳过提权，游戏可能仍收不到按键（可手动右键 → 以管理员身份运行）。", "Elevation skipped; the game may still not receive keys (right-click → Run as administrator)."},
	"title_priv":       {"%s · 权限: %s", "%s · privilege: %s"},
	"ilv_high":         {"管理员", "admin"},
	"ilv_medium":       {"标准", "standard"},
	"ilv_mediump":      {"标准+", "standard+"},
	"ilv_low":          {"低", "low"},
	"ilv_system":       {"系统", "system"},
	"ilv_protect":      {"受保护", "protected"},
	"ilv_unknown":      {"未知", "unknown"},
}
