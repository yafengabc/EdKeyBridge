package main

import "testing"

func TestTOMLRoundTrip(t *testing.T) {
	s := Settings{
		Lang: "auto", KeysDig: true, KeysLet: true, KeysSpec: false,
		HoldMs: 120, Reinject: true, Scancode: false, Diagnostic: true,
		Autostart: false, Verbose: true,
	}
	ser := serializeTOML(s)
	m := parseTOML(ser)
	if got := m["lang"]; got != `"auto"` {
		t.Fatalf("lang = %q", got)
	}
	if got := m["hold_ms"]; got != "120" {
		t.Fatalf("hold_ms = %q", got)
	}
	if got := m["reinject"]; got != "true" {
		t.Fatalf("reinject = %q", got)
	}
	if got := m["keys_digits"]; got != "true" {
		t.Fatalf("keys_digits = %q", got)
	}
	if got := m["keys_special"]; got != "false" {
		t.Fatalf("keys_special = %q", got)
	}
	if _, ok := m["http"]; ok {
		t.Fatalf("不应存在 http 字段")
	}
	loaded := settingsFromMap(m)
	if !loaded.KeysDig || !loaded.KeysLet || loaded.KeysSpec ||
		loaded.HoldMs != 120 || !loaded.Reinject ||
		!loaded.Diagnostic || loaded.Autostart ||
		!loaded.Verbose || loaded.Lang != "auto" {
		t.Fatalf("roundtrip mismatch: %+v", loaded)
	}
}

func TestParseCommentAndQuote(t *testing.T) {
	m := parseTOML("# 这是注释\nkeys = \"a#b\" # 行尾注释\nhold_ms = 50\n")
	if m["keys"] != `"a#b"` {
		t.Fatalf("keys = %q", m["keys"])
	}
	if m["hold_ms"] != "50" {
		t.Fatalf("hold_ms = %q", m["hold_ms"])
	}
}

// TestLegacyKeysField 验证旧版 keys 单串字段的兼容映射。
func TestLegacyKeysField(t *testing.T) {
	m := parseTOML("keys = \"all\"")
	s := settingsFromMap(m)
	if !s.KeysDig || !s.KeysLet || !s.KeysSpec {
		t.Fatalf("keys=all 应三组全开: %+v", s)
	}
	m = parseTOML("keys = \"wasd\"")
	s = settingsFromMap(m)
	if s.KeysDig || !s.KeysLet || s.KeysSpec {
		t.Fatalf("keys=wasd 应仅字母开: %+v", s)
	}
	m = parseTOML("keys = \"\"")
	s = settingsFromMap(m)
	if s.KeysDig || s.KeysLet || s.KeysSpec {
		t.Fatalf("keys=空 应三组全关: %+v", s)
	}
}

func TestLoadDefaultsWhenMissing(t *testing.T) {
	s := settingsFromMap(map[string]string{})
	// 默认：启动即开始桥接（Autostart=true），三组按键全开，最小按下 80ms
	if s.Lang != "auto" || s.HoldMs != 80 || !s.Reinject ||
		!s.KeysDig || !s.KeysLet || !s.KeysSpec || !s.Autostart {
		t.Fatalf("defaults wrong: %+v", s)
	}
}
