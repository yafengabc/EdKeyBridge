package main

import "testing"

func TestResolveLang(t *testing.T) {
	if resolveLang("zh") != "zh" {
		t.Fatal("zh 应解析为中文")
	}
	if resolveLang("en") != "en" {
		t.Fatal("en 应解析为英文")
	}
	r := resolveLang("auto")
	if r != "zh" && r != "en" {
		t.Fatal("auto 必须解析为 zh 或 en")
	}
	if resolveLang("") != r {
		t.Fatal("空设置应等同于 auto")
	}
	if resolveLang("fr") != resolveLang("auto") {
		t.Fatal("未知语言应回退为系统语言(auto)")
	}
}

func TestT(t *testing.T) {
	curLang = "zh"
	if T("start") != "开始" {
		t.Fatalf("zh start = %q", T("start"))
	}
	curLang = "en"
	if T("start") != "Start" {
		t.Fatalf("en start = %q", T("start"))
	}
	if T("missing_key") != "missing_key" {
		t.Fatal("缺失 key 应回退为 key 本身")
	}
}
