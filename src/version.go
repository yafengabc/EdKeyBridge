package main

import (
	"runtime/debug"
	"strings"
)

// version is injected at build time via
//   -ldflags "-X main.version=$(git describe --tags --always)"
// and falls back to "dev" when not set.
//
// 坑：main 包的 -X 只认 **main.<var>**，写成 import path（edkeybridge/src.version、
// 或源码在根目录时的 edkeybridge.version）不会报错，但注入静默失效，版本一直是 dev。
var version = "dev"

func vcsInfo() (rev, committed string, modified bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			committed = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if len(committed) >= 10 {
		committed = committed[:10] // YYYY-MM-DD
	}
	return rev, committed, modified
}

// versionInfo assembles a human-readable version string, e.g.
// "v1.0.0 (8c0fe73, 2026-09-15, clean)".
func versionInfo() string {
	rev, committed, modified := vcsInfo()

	head := version
	if head == "" {
		head = rev // untagged build: the revision IS the identity
	}

	var extra []string
	// git describe --tags already carries the hash; printing it again is noise.
	if rev != "" && !strings.Contains(head, rev) {
		extra = append(extra, rev)
	}
	if committed != "" {
		extra = append(extra, committed)
	}
	if rev != "" {
		if modified {
			extra = append(extra, "dirty")
		} else {
			extra = append(extra, "clean")
		}
	}

	if head == "" {
		head = "dev"
	}
	if len(extra) == 0 {
		return head
	}
	return head + " (" + strings.Join(extra, ", ") + ")"
}
