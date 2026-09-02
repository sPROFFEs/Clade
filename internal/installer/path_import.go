package installer

// User-PATH hydration for desktop launches.
//
// When PrAImate is launched from a Linux .desktop shortcut (or a Wails
// app on macOS, or a Start-menu entry on Windows) the process inherits
// the desktop session's PATH — NOT what the user's shell rc would
// produce. So a CLI installed into ~/.bun/bin / ~/.cargo/bin /
// ~/.local/bin won't resolve via exec.LookPath unless the user has
// logged out + back in. This file fixes that: at startup (and after
// every install) we scan a fixed list of well-known user-level install
// dirs and prepend whichever ones exist on disk.
//
// Each entry is idempotent: re-running is a no-op. Safe to call from
// both the TUI and GUI mains. Pairs with ImportPnpmPathIfPresent,
// ImportManagedToolsToPath, ImportPraimateBinToPath — those handle
// PrAImate-managed prefixes; this one handles dirs the user (or other
// installers) wrote to.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ImportUserBinDirs prepends the well-known per-user CLI install dirs
// to PATH when they exist. Covers bun, deno, cargo, go, npm-global,
// volta, foundry, rye, Homebrew on Apple Silicon, ~/.local/bin, and
// the WinGet / npm-on-Windows dirs.
//
// Repeatable; entries already on PATH are skipped.
func ImportUserBinDirs() {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	dirs := candidateUserBinDirs(home)
	prependIfPresent(dirs)
}

// ImportManagedAgentsToPath prepends the executable directories for agents
// installed into PrAImate-owned prefixes. OpenClaude currently uses this
// layout because its dependency tree must be installed locally rather than as
// a global npm package. Keeping the prefix on PATH makes the CLI adapters,
// terminal launcher, and picker detection agree with DetectAgents.
func ImportManagedAgentsToPath() {
	dir, err := ManagedAgentBinDir("openclaude")
	if err != nil {
		return
	}
	prependIfPresent([]string{dir})
}

func candidateUserBinDirs(home string) []string {
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		appd := os.Getenv("APPDATA")
		dirs := []string{
			// Claude Code's native installer targets ~/.local/bin on
			// Windows too, and bun's PowerShell installer uses ~/.bun/bin
			// (NOT %LOCALAPPDATA%\Programs\bun).
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".deno", "bin"),
			filepath.Join(home, "go", "bin"),
			// scoop's default per-user shim dir (deepseek-tui et al).
			filepath.Join(home, "scoop", "shims"),
		}
		if local != "" {
			dirs = append(dirs,
				filepath.Join(local, "Programs", "bun", "bin"),
				filepath.Join(local, "Microsoft", "WinGet", "Links"),
			)
		}
		if appd != "" {
			dirs = append(dirs, filepath.Join(appd, "npm"))
		}
		return dirs
	case "darwin":
		return []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".deno", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, ".rye", "shims"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".foundry", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
		}
	default:
		return []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".deno", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, ".rye", "shims"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".foundry", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			// Homebrew on Linux.
			"/home/linuxbrew/.linuxbrew/bin",
		}
	}
}

func prependIfPresent(dirs []string) {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	eq := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		eq = strings.EqualFold
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		path := os.Getenv("PATH")
		dup := false
		for _, entry := range strings.Split(path, sep) {
			if eq(strings.TrimRight(entry, `\/`), strings.TrimRight(d, `\/`)) {
				dup = true
				break
			}
		}
		if !dup {
			_ = os.Setenv("PATH", d+sep+path)
		}
	}
}
