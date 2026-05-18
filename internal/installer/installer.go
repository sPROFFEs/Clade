// Package installer ports the agent-cli-installer.sh logic to Go so the
// launcher can install / update agent CLIs without shelling out to Bash
// (which isn't installed by default on Windows). Every method is a single
// shell-style command line; we expose them to the TUI as a sorted catalog
// with prerequisite checks.
package installer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	AgentGemini   AgentID = "gemini"
	AgentDeepSeek AgentID = "deepseek"
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

// autoFixablePrereqs lists prereqs the launcher can install for the user
// without invasive system changes. Run() resolves these automatically
// before running the install method. Anything not in this set requires
// user action (e.g. installing Node).
var autoFixablePrereqs = map[string]bool{
	"pnpm": true,
}

// AutoFixable returns the subset of `missing` that Run() will resolve
// automatically. The TUI uses this to render "will auto-install: X"
// instead of blocking the user.
func AutoFixable(missing []string) []string {
	var out []string
	for _, p := range missing {
		if autoFixablePrereqs[p] {
			out = append(out, p)
		}
	}
	return out
}

// UnfixableMissing returns the prereqs in `missing` that the user must
// install themselves (Node, mostly). The TUI blocks Enter when this is
// non-empty.
func UnfixableMissing(missing []string) []string {
	var out []string
	for _, p := range missing {
		if !autoFixablePrereqs[p] {
			out = append(out, p)
		}
	}
	return out
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

// pnpmGlobalBinDir asks pnpm where it puts global bins. Returns an empty
// string when pnpm hasn't been set up yet — pnpm prints "undefined" in
// that case, which we normalize. Note: pnpm reads PNPM_HOME from this
// process's env, so a value written by an earlier `pnpm setup` (which
// lands in the Windows user registry / shell rc) is invisible until the
// process restarts. EnsurePnpmReady handles that gap.
func pnpmGlobalBinDir(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "pnpm", "config", "get", "global-bin-dir").Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "undefined" {
		return ""
	}
	return s
}

// ImportPnpmPathIfPresent looks for the OS-default pnpm bin dir on disk;
// if it exists but isn't already in PATH, prepends it (and exports
// PNPM_HOME). Use this at launcher startup so a previously-installed
// agent (via `pnpm add -g …` in some past session) is found by
// exec.LookPath even when the user's shell hasn't picked up the env
// vars `pnpm setup` wrote to the Windows registry / shell rc.
//
// Safe to call repeatedly: no-op when the dir is already in PATH.
func ImportPnpmPathIfPresent() {
	dir := defaultPnpmHome()
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return // pnpm setup has never run
	}
	path := os.Getenv("PATH")
	// Case-insensitive compare on Windows (paths in PATH may have mixed case).
	cmp := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		cmp = strings.EqualFold
	}
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	for _, entry := range strings.Split(path, sep) {
		if cmp(strings.TrimRight(entry, `\/`), strings.TrimRight(dir, `\/`)) {
			return // already on PATH
		}
	}
	_ = os.Setenv("PATH", dir+sep+path)
	if os.Getenv("PNPM_HOME") == "" {
		_ = os.Setenv("PNPM_HOME", dir)
	}
}

// defaultPnpmHome returns the OS-default location pnpm setup writes to.
// Used as a fallback when neither pnpm config get nor parsing the setup
// output gives us a value.
//
//	Windows: %LOCALAPPDATA%\pnpm
//	macOS:   ~/Library/pnpm
//	Linux:   $XDG_DATA_HOME/pnpm or ~/.local/share/pnpm
func defaultPnpmHome() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "pnpm")
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Local", "pnpm")
		}
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "pnpm")
		}
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "pnpm")
		}
		if home != "" {
			return filepath.Join(home, ".local", "share", "pnpm")
		}
	}
	return ""
}

var pnpmHomeRE = regexp.MustCompile(`(?m)^\s*PNPM_HOME\s*=\s*(.+?)\s*$`)

// extractPnpmHome scrapes the value out of `pnpm setup` output, which
// includes a line like:
//
//	PNPM_HOME=C:\Users\user\AppData\Local\pnpm
//
// on Windows and similar on other platforms.
func extractPnpmHome(setupStdout string) string {
	m := pnpmHomeRE.FindStringSubmatch(setupStdout)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// EnsurePnpmReady makes pnpm fully usable for a `pnpm add -g …` command
// in the same process:
//
//  1. If pnpm is missing, runs `corepack enable`.
//  2. If pnpm has no global-bin-dir configured (the "ERR_PNPM_NO_GLOBAL_BIN_DIR"
//     case on a fresh Windows install), runs `pnpm setup`.
//  3. Resolves PNPM_HOME using three sources in order:
//     a) `pnpm config get global-bin-dir`  (post-restart-of-shell case)
//     b) parse PNPM_HOME from the just-captured pnpm setup stdout
//     c) the OS-default location (%LOCALAPPDATA%\pnpm etc.)
//  4. Returns env additions (PNPM_HOME, PATH) so the upcoming install
//     command in *this* process sees the new bin dir without needing a
//     shell restart.
func EnsurePnpmReady(ctx context.Context, w io.Writer) ([]string, error) {
	if _, err := exec.LookPath("pnpm"); err != nil {
		fmt.Fprintln(w, "→ pnpm not on PATH; enabling via corepack...")
		if err := AutoInstallPnpm(ctx, w); err != nil {
			return nil, err
		}
	}

	binDir := pnpmGlobalBinDir(ctx)
	if binDir == "" {
		fmt.Fprintln(w, "→ pnpm global bin dir not configured; running `pnpm setup`...")
		var setupCap bytes.Buffer
		cmd := exec.CommandContext(ctx, "pnpm", "setup")
		cmd.Stdout = io.MultiWriter(w, &setupCap)
		cmd.Stderr = io.MultiWriter(w, &setupCap)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("pnpm setup: %w", err)
		}
		// pnpm setup writes to user-level env (registry on Windows, rc
		// files on Unix) — those don't propagate to our running process.
		// Try config first, fall back to parsing setup output, then to
		// the OS default.
		binDir = pnpmGlobalBinDir(ctx)
		if binDir == "" {
			binDir = extractPnpmHome(setupCap.String())
		}
		if binDir == "" {
			binDir = defaultPnpmHome()
		}
		if binDir == "" {
			return nil, fmt.Errorf("pnpm setup ran but couldn't resolve PNPM_HOME; restart your shell and retry")
		}
		// pnpm setup creates the dir; verify it's there.
		if _, err := os.Stat(binDir); err != nil {
			return nil, fmt.Errorf("pnpm setup ran but %s doesn't exist: %w", binDir, err)
		}
		fmt.Fprintf(w, "✓ pnpm bin dir: %s\n", binDir)
	}

	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	return []string{
		"PNPM_HOME=" + binDir,
		"PATH=" + binDir + sep + os.Getenv("PATH"),
	}, nil
}

// Run executes the method's command line. stdout/stderr are streamed live
// so the TUI can render output as it arrives. Returns the OS exit error
// (nil on exit 0).
//
// For pnpm methods, Run auto-resolves the two common Windows footguns
// (pnpm not installed, pnpm setup not run) and injects PNPM_HOME + PATH
// into the install command's env. That makes `pnpm add -g …` work
// end-to-end without a shell restart.
func Run(ctx context.Context, m Method, stdout, stderr io.Writer) error {
	var extraEnv []string
	if strings.HasPrefix(m.Command, "pnpm ") {
		env, err := EnsurePnpmReady(ctx, stdout)
		if err != nil {
			return err
		}
		extraEnv = env
	}
	cmd := buildCmd(ctx, m)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
		// Also mirror the additions onto OUR process env so subsequent
		// exec.LookPath calls (the post-install agent re-detection, the
		// eventual launch) see the new pnpm bin dir without needing the
		// user to restart their shell.
		for _, kv := range extraEnv {
			if i := strings.Index(kv, "="); i > 0 {
				_ = os.Setenv(kv[:i], kv[i+1:])
			}
		}
	}
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
			// Offer the official curl|bash installer on Windows too,
			// when a Bash shell is on PATH (Git Bash / WSL / Cygwin).
			// The opencode-ai npm package has shipped Windows binaries
			// that some hosts reject ("not compatible with this version
			// of Windows"); the official script downloads a native
			// build that works around it. Recommend when both bash and
			// curl exist.
			methods := []Method{
				{ID: "curl", Label: "Official install script (needs bash + curl)",
					Command: "curl -fsSL https://opencode.ai/install | bash",
					Shell:   ShellBash, Recommended: true},
				{ID: "pnpm", Label: "pnpm global package",
					Command: pnpmPkg("opencode-ai"),
					Prereqs: []string{"node", "pnpm"}},
			}
			// If bash isn't on PATH the curl method will be filtered
			// out; demote pnpm to recommended in that case.
			if _, err := exec.LookPath("bash"); err != nil {
				methods = methods[1:]
				methods[0].Recommended = true
			}
			return methods
		}

	case AgentGemini:
		switch current {
		case OSMacOS:
			return []Method{
				{ID: "brew", Label: "Homebrew formula", Command: formulaCmd(action, "gemini-cli"), Recommended: true},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("@google/gemini-cli"), Prereqs: []string{"node", "pnpm"}},
			}
		case OSLinux, OSWSL:
			return []Method{
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("@google/gemini-cli"), Recommended: true, Prereqs: []string{"node", "pnpm"}},
				{ID: "brew", Label: "Homebrew/Linuxbrew formula", Command: formulaCmd(action, "gemini-cli")},
			}
		case OSWindows:
			return []Method{
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("@google/gemini-cli"), Recommended: true, Prereqs: []string{"node", "pnpm"}},
			}
		}

	case AgentDeepSeek:
		// DeepSeek-TUI ships through five channels. We prefer the
		// native package manager per OS (brew/scoop), fall back to
		// npm (which works everywhere with Node), and offer cargo
		// when Rust is available for users who'd rather build.
		switch current {
		case OSMacOS:
			return []Method{
				{ID: "brew", Label: "Homebrew tap", Command: deepseekBrewCmd(action), Recommended: true},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("deepseek-tui"), Prereqs: []string{"node", "pnpm"}},
				{ID: "cargo", Label: "cargo install", Command: deepseekCargoCmd(action), Prereqs: []string{"cargo"}},
			}
		case OSLinux, OSWSL:
			return []Method{
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("deepseek-tui"), Recommended: true, Prereqs: []string{"node", "pnpm"}},
				{ID: "cargo", Label: "cargo install", Command: deepseekCargoCmd(action), Prereqs: []string{"cargo"}},
				// Homebrew on Linux uses the same tap as macOS.
				{ID: "brew", Label: "Homebrew tap", Command: deepseekBrewCmd(action)},
			}
		case OSWindows:
			return []Method{
				{ID: "scoop", Label: "Scoop package", Command: deepseekScoopCmd(action), Recommended: true},
				{ID: "pnpm", Label: "pnpm global package", Command: pnpmPkg("deepseek-tui"), Prereqs: []string{"node", "pnpm"}},
				{ID: "cargo", Label: "cargo install", Command: deepseekCargoCmd(action), Prereqs: []string{"cargo"}},
			}
		}
	}
	return nil
}

// deepseekBrewCmd returns the brew install/upgrade command. The
// upstream tap is "Hmbown/deepseek-tui"; once tapped, brew refers to
// it as "deepseek-tui" so install / upgrade only need that name.
func deepseekBrewCmd(a Action) string {
	if a == ActionUpdate {
		// brew refreshes the tap automatically on `upgrade`.
		return "brew upgrade deepseek-tui"
	}
	return "brew tap Hmbown/deepseek-tui && brew install deepseek-tui"
}

// deepseekScoopCmd returns the scoop install/update command. Scoop
// uses different verbs from brew/winget so we keep this isolated.
func deepseekScoopCmd(a Action) string {
	if a == ActionUpdate {
		return "scoop update deepseek-tui"
	}
	return "scoop install deepseek-tui"
}

// deepseekCargoCmd installs both crates the project publishes — the
// dispatcher (deepseek-tui) and the CLI core (deepseek-tui-cli).
// --locked uses the Cargo.lock that ships with each crate so
// dependency drift can't break the build mid-install.
func deepseekCargoCmd(a Action) string {
	if a == ActionUpdate {
		// `cargo install --force` re-installs over the existing
		// binaries, which is how cargo idiomatically does upgrades.
		return "cargo install --locked --force deepseek-tui-cli && cargo install --locked --force deepseek-tui"
	}
	return "cargo install --locked deepseek-tui-cli && cargo install --locked deepseek-tui"
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
