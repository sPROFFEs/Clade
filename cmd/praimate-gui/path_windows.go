//go:build windows

package main

// Windows PATH propagation — when the user installs a CLI via an
// installer that writes the user's PATH registry key (npm prefix,
// pnpm setup, bun installer, scoop, winget bucket dirs, etc.) the
// change doesn't reach already-running processes. praimate-gui
// launched from a desktop shortcut at 9am won't see opencode that
// got installed at 10am — even though a fresh PowerShell window
// does. We work around it by reading the registry directly and
// merging any new directories into our os.Environ() PATH.

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// importWindowsRegistryPath merges the User and Machine PATH from
// the Windows registry into the process's current PATH. Dedupe is
// case-insensitive (Windows). Safe to call repeatedly — no-op when
// nothing new appears.
func importWindowsRegistryPath() {
	merged := os.Getenv("PATH")
	have := map[string]bool{}
	for _, p := range strings.Split(merged, ";") {
		if p = strings.TrimSpace(p); p != "" {
			have[strings.ToLower(p)] = true
		}
	}
	add := func(extra string) {
		for _, p := range strings.Split(extra, ";") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if have[strings.ToLower(p)] {
				continue
			}
			merged = p + ";" + merged
			have[strings.ToLower(p)] = true
		}
	}
	add(readRegPath(registry.CURRENT_USER, `Environment`))
	add(readRegPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`))
	_ = os.Setenv("PATH", merged)
}

func readRegPath(root registry.Key, sub string) string {
	k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	// Some keys store PATH as REG_EXPAND_SZ — GetStringValue handles
	// both REG_SZ and REG_EXPAND_SZ without env-var expansion. We
	// expand manually below so %USERPROFILE% in the registry resolves
	// to the actual home dir.
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return ""
	}
	if exp, err := registry.ExpandString(v); err == nil {
		return exp
	}
	return v
}
