//go:build windows

package installer

// Windows PATH propagation — when the user installs a CLI via an
// installer that writes the user's PATH registry key (npm prefix,
// pnpm setup, bun installer, scoop, winget bucket dirs, etc.) the
// change doesn't reach already-running processes — and doesn't even
// reach NEW processes spawned from a terminal that predates the
// install. Reading the registry directly and merging any new
// directories into our PATH fixes detection for both the GUI
// (desktop-shortcut launch) and the TUI (stale terminal session).

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// ImportWindowsRegistryPath merges the User and Machine PATH from the
// Windows registry into the process's current PATH. Dedupe is
// case-insensitive (Windows). Safe to call repeatedly — no-op when
// nothing new appears.
func ImportWindowsRegistryPath() {
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
	// both REG_SZ and REG_EXPAND_SZ without env-var expansion. Expand
	// manually so %USERPROFILE% in the registry resolves.
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return ""
	}
	if exp, err := registry.ExpandString(v); err == nil {
		return exp
	}
	return v
}
