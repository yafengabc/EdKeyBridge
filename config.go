package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Settings 是 EdKeyBridge 的全部配置项，持久化到 TOML。
type Settings struct {
	Lang      string // auto / zh / en
	KeysDig   bool   // 转换数字 (0-9)
	KeysLet   bool   // 转换字母 (A-Z)
	KeysSpec  bool   // 转换特殊按键 (空格/符号等)
	HoldMs    int    // 最小按下时长(ms)
	Reinject  bool   // 对注入的真实按键也吞掉+重发
	Scancode  bool   // 用扫描码注入而非虚拟键码
	Diagnostic bool  // 诊断模式：只观察不转换
	Autostart bool   // 启动即开始桥接
	Verbose   bool   // 详细日志
}

// cfg 是当前生效的全局配置，由 main 在启动时加载，GUI 实时修改。
var cfg = defaultSettings()

func defaultSettings() Settings {
	return Settings{
		Lang:      "auto",
		KeysDig:   true,
		KeysLet:   true,
		KeysSpec:  true,
		HoldMs:    80,
		Reinject:  true,
		Scancode:  false,
		Diagnostic: false,
		Autostart: true, // 启动即开始桥接（可在界面里取消勾选并保存）
		Verbose:   false,
	}
}

// configPath 返回与 exe 同目录的 edkeybridge.toml 路径。
func configPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "edkeybridge.toml")
	}
	return "edkeybridge.toml"
}

// stripComment 去掉 '#' 之后的内容，但忽略出现在双引号字符串内的 '#'。
func stripComment(s string) string {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inQuote = !inQuote
		} else if c == '#' && !inQuote {
			return s[:i]
		}
	}
	return s
}

// parseTOML 是最小 TOML 解析器：支持注释、key = value，
// 值类型支持引号字符串 / 整数 / 布尔。忽略 [section] 行。
func parseTOML(data string) map[string]string {
	m := map[string]string{}
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(stripComment(strings.TrimRight(raw, "\r")))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue // 跳过分区头
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		m[key] = val
	}
	return m
}

// unquote 去掉值两侧引号（若有）并反转义 \" 与 \\。
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		inner := v[1 : len(v)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	return v
}

// parseKeysGroups 兼容旧版 keys 字段：
//   "all"/"*" -> 三组全开；"" -> 三组全关；具体字符 -> 按类别分组。
func parseKeysGroups(s string) (dig, let, spec bool) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "all", "*":
		return true, true, true
	case "":
		return false, false, false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			dig = true
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			let = true
		default:
			spec = true
		}
	}
	return
}

// settingsFromMap 把解析出的扁平 map 合并进默认配置。
func settingsFromMap(m map[string]string) Settings {
	s := defaultSettings()
	if v, ok := m["lang"]; ok {
		s.Lang = unquote(v)
	}
	if v, ok := m["keys"]; ok {
		// 旧版兼容：单字符串按键集合
		s.KeysDig, s.KeysLet, s.KeysSpec = parseKeysGroups(unquote(v))
	} else {
		if v, ok := m["keys_digits"]; ok {
			s.KeysDig = parseBool(v)
		}
		if v, ok := m["keys_letters"]; ok {
			s.KeysLet = parseBool(v)
		}
		if v, ok := m["keys_special"]; ok {
			s.KeysSpec = parseBool(v)
		}
	}
	if v, ok := m["hold_ms"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			s.HoldMs = n
		}
	}
	if v, ok := m["reinject"]; ok {
		s.Reinject = parseBool(v)
	}
	if v, ok := m["scancode"]; ok {
		s.Scancode = parseBool(v)
	}
	if v, ok := m["diagnostic"]; ok {
		s.Diagnostic = parseBool(v)
	}
	if v, ok := m["autostart"]; ok {
		s.Autostart = parseBool(v)
	}
	if v, ok := m["verbose"]; ok {
		s.Verbose = parseBool(v)
	}
	return s
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// serializeTOML 生成带注释的 TOML 文本。
func serializeTOML(s Settings) string {
	var b strings.Builder
	b.WriteString("# EdKeyBridge 配置 / EdKeyBridge config\n")
	b.WriteString("# lang: auto(跟随系统) / zh(中文) / en(英文)\n")
	b.WriteString("# keys_digits/letters/special: 勾选需要转换的按键分组\n\n")
	fmt.Fprintf(&b, "lang = %q\n", s.Lang)
	fmt.Fprintf(&b, "keys_digits = %v\n", s.KeysDig)
	fmt.Fprintf(&b, "keys_letters = %v\n", s.KeysLet)
	fmt.Fprintf(&b, "keys_special = %v\n", s.KeysSpec)
	fmt.Fprintf(&b, "hold_ms = %d\n", s.HoldMs)
	fmt.Fprintf(&b, "reinject = %v\n", s.Reinject)
	fmt.Fprintf(&b, "scancode = %v\n", s.Scancode)
	fmt.Fprintf(&b, "diagnostic = %v\n", s.Diagnostic)
	fmt.Fprintf(&b, "autostart = %v\n", s.Autostart)
	fmt.Fprintf(&b, "verbose = %v\n", s.Verbose)
	return b.String()
}

// loadSettings 读取配置文件；不存在时返回默认值。
func loadSettings(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings()
	}
	return settingsFromMap(parseTOML(string(data)))
}

// saveSettings 写回配置文件。
func saveSettings(path string, s Settings) error {
	return os.WriteFile(path, []byte(serializeTOML(s)), 0644)
}
