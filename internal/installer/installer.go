// Package installer ports the agent-cli-installer.sh logic to Go so the
// launcher can install / update agent CLIs without shelling out to Bash
// (which isn't installed by default on Windows). Every method is a single
// shell-style command line; we expose them to the TUI as a sorted catalog
// with prerequisite checks.
package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OS is the launcher's runtime classification. Distinct from runtime.GOOS
// because we treat WSL as different from native Linux (some install
// scripts gate on it).
type OS string

const (
	OSMacOS   OS = "macos"
	OSLinux   OS = "linux"
	OSWSL     OS = "wsl"
	OSWindows OS = "windows"
)

// DetectOS returns the launcher's OS classification. Cheap; safe to call
// once per Update.
func DetectOS() OS {
	switch runtime.GOOS {
	case "darwin":
		return OSMacOS
	case "windows":
		return OSWindows
	case "linux":
		if isWSL() {
			return OSWSL
		}
		return OSLinux
	default:
		return OS(runtime.GOOS)
	}
}

func isWSL() bool {
	for _, p := range []string{"/proc/version", "/proc/sys/kernel/osrelease"} {
		raw, err := os.ReadFile(p)
		if err == nil && strings.Contains(strings.ToLower(string(raw)), "microsoft") {
			return true
		}
	}
	return false
}

// Method is one way to install or update an agent. It carries enough
// context for the TUI to show "we're about to run: <cmd>" before doing
// anything.
type Method struct {
	ID          string   // "curl", "pnpm", "brew", "winget", "powershell"
	Label       string   // human-readable line shown in the picker
	Command     string   // single shell-style command line
	Shell       Shell    // how to invoke Command
	Recommended bool     // exactly one method per (agent,action,os) is the default
	Prereqs     []string // names probed before running ("node", "pnpm")
}

// Shell tells Run how to execute Command. Direct means no shell wrapping
// (first word is the binary, rest are args, split on spaces — safe for
// the simple commands in our catalog).
type Shell string

const (
	ShellDirect     Shell = ""
	ShellBash       Shell = "bash"
	ShellPowerShell Shell = "powershell"
)

// AgentID matches launcher.AgentID. Kept as a separate string here so
// this package doesn't depend on the launcher package.
type AgentID string

const (
	AgentClaude   AgentID = "claude"
	AgentCodex    AgentID = "codex"
	AgentOpenCode AgentID = "opencode"
)

// Action is "install" or "update". Update reuses most install commands,
// just with @latest where applicable.
type Action string

const (
	ActionInstall Action = "install"
	ActionUpdate  Action = "update"
)

// Methods returns the candidate install/update methods for an agent on
// the current OS, filtered to only those whose required package manager
// exists on PATH. The first entry in the slice is the recommended one.
func Methods(agent AgentID, action Action, current OS) []Method {
	all := allMethods(agent, action, current)
	var filtered []Method
	for _, m := range all {
		if !methodAvailable(m) {
			continue
		}
		filtered = append(filtered, m)
	}
	// Promote the recommended one to index 0 if it survived filtering.
	for i, m := range filtered {
		if m.Recommended {
			if i != 0 {
				filtered[0], filtered[i] = filtered[i], filtered[0]
			}
			break
		}
	}
	return filtered
}

// methodAvailable reports whether the package manager named by m.ID is on
// PATH. The shell wrappers (curl|bash, powershell) only need curl /
// powershell, which we model as the ID itself.
func methodAvailable(m Method) bool {
	bin := m.ID
	switch bin {
	case "powershell":
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			return true
		}
		_, err := exec.LookPath("powershell")
		return err == nil
	default:
		_, err := exec.LookPath(bin)
		return err == nil
	}
}

// PrereqsMissing returns the subset of m.Prereqs not found on PATH. Used
// by the TUI to surface a "this method needs <x>" warning before running.
func PrereqsMissing(m Method) []string {
	var missing []string
	for _, p := range m.Prereqs {
		if _, err := exec.LookPath(p); err != nil {
			missing = append(missing, p)
		}
	}
	return missing
}

// AutoInstallPnpm runs `corepack enable` so pnpm becomes available
// without an extra global install. Requires Node ≥ 16.10, which ships
// corepack. Returns a clear error if Node isn't on PATH.
func AutoInstallPnpm(ctx context.Context, w io.Writer) error {
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node is required to auto-install pnpm via corepack — install Node ≥ 20 first")
	}
	if _, err := exec.LookPath("corepack"); err != nil {
		return fmt.Errorf("corepack not on PATH — install a Node version that bundles corepack (≥ 16.10)")
	}
	cmd := exec.CommandContext(ctx, "corepack", "enable")
	cmd.Stdout, cmd.Stderr = w, w
	return cmd.Run()
}

// Run executes the method's command line. stdout/stderr are streamed live
// so the TUI can render output as it arrives. Returns the OS exit error
// (nil on exit 0).
func Run(ctx context.Context, m Method, stdout, stderr io.Writer) error {
	cmd := buildCmd(ctx, m)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func buildCmd(ctx context.Context, m Method) *exec.Cmd {
	switch m.Shell {
	case ShellBash:
		return exec.CommandContext(ctx, "bash", "-c", m.Command)
	case ShellPowerShell:
		bin := "powershell"
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			bin = "powershell.exe"
		}
		return exec.CommandContext(ctx, bin, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", m.Command)
	default:
		parts := strings.Fields(m.Command)
		return exec.CommandContext(ctx, parts[0], parts[1:]...)
	}
}

// allMethods is the static catalog, before filtering by what's installed.
// Mirror of agent-cli-installer.sh but pnpm-first and Windows-aware.
func allMethods(agent AgentID, action Action, current OS) []Method {
	pnpmPkg := func(pkg string) string {
		if action == ActionUpdate {
			return "pnpm add -g " + pkg + "@latest"
		}
		return "pnpm add -g " + pkg
	}

	switch agent {
	case AgentClaude:
		switch current {
		case OSMacOS:
			return []Method{
				{ID: "brew", Label: "Homebrew cask", Command: caskCmd(action, "claude-code"), Recommended: true},
				{ID: "curl", Label: "Official install script", Command: "curl -fsSL https://claude.ai/install.sh | bash", Shell: ShellBash},
			}
		case OSLinux, OSWSL:
			return []Method{
				{ID: "curl", Label: "Official install script", Command: "curl -fsSL https://claude.ai/install.sh | bash", Shell: ShellBash, Recommended: true},
			}
		case OSWindows:
			return []Method{
				{ID: "winget", Label: "winget package", Command: wingetCmd(action, "Anthropic.ClaudeCode"), Recommended: true},
				{ID: "powershell", Label: "PowerShell installer", Command: "irm https://claude.ai/install.ps1 | iex", Shell: ShellPowerShell},
			}
		}

	case AgentCodex:
		switch current {
		case OSMacOS:
			return []Method{
				{ID: "brew", Label: "Homebrew formula", Command: formulaCmd(action, "codex"), Recommended: true},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("@openai/codex"), Prereqs: []string{"node", "pnpm"}},
			}
		case OSLinux, OSWSL, OSWindows:
			return []Method{
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("@openai/codex"), Recommended: true, Prereqs: []string{"node", "pnpm"}},
				{ID: "brew", Label: "Homebrew formula", Command: formulaCmd(action, "codex")},
			}
		}

	case AgentOpenCode:
		switch current {
		case OSMacOS:
			return []Method{
				{ID: "curl", Label: "Official install script", Command: "curl -fsSL https://opencode.ai/install | bash", Shell: ShellBash, Recommended: true},
				{ID: "brew", Label: "Homebrew tap", Command: tapCmd(action, "anomalyco/tap/opencode")},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("opencode-ai"), Prereqs: []string{"node", "pnpm"}},
			}
		case OSLinux, OSWSL:
			return []Method{
				{ID: "curl", Label: "Official install script", Command: "curl -fsSL https://opencode.ai/install | bash", Shell: ShellBash, Recommended: true},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("opencode-ai"), Prereqs: []string{"node", "pnpm"}},
				{ID: "paru", Label: "AUR via paru", Command: paruCmd(action, "opencode")},
			}
		case OSWindows:
			return []Method{
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("opencode-ai"), Recommended: true, Prereqs: []string{"node", "pnpm"}},
			}
		}
	}
	return nil
}

func caskCmd(a Action, name string) string {
	if a == ActionUpdate {
		return "brew upgrade --cask " + name
	}
	return "brew install --cask " + name
}

func formulaCmd(a Action, name string) string {
	if a == ActionUpdate {
		return "brew upgrade " + name
	}
	return "brew install " + name
}

func tapCmd(a Action, name string) string {
	if a == ActionUpdate {
		return "brew upgrade " + name
	}
	return "brew install " + name
}

func wingetCmd(a Action, id string) string {
	if a == ActionUpdate {
		return "winget upgrade --id " + id + " -e"
	}
	return "winget install --id " + id + " -e --accept-package-agreements --accept-source-agreements"
}

func paruCmd(a Action, name string) string {
	if a == ActionUpdate {
		return "paru -Syu " + name
	}
	return "paru -S " + name
}
