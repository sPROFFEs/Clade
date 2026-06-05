package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sPROFFEs/Clade/internal/installer"
)

// AgentID is one of "claude", "codex", "opencode", "gemini", "deepseek".
type AgentID string

const (
	AgentClaude     AgentID = "claude"
	AgentOpenClaude AgentID = "openclaude"
	AgentCodex      AgentID = "codex"
	AgentOpenCode   AgentID = "opencode"
	AgentGemini     AgentID = "gemini"
	AgentDeepSeek   AgentID = "deepseek"
)

// Agent describes one supported CLI agent. WpcTarget is the wpc target
// the launcher uses to compile the workpath into the sandbox before
// launching this agent.
type Agent struct {
	ID        AgentID
	Label     string
	Binary    string
	WpcTarget string // "claude" or "codex"
	// Available is true only when the binary is on PATH AND `--version`
	// exits cleanly. We require the version probe to succeed because a
	// binary that exists but crashes on launch (e.g. an opencode-ai npm
	// package shipped for an incompatible Windows build) shouldn't appear
	// launchable.
	Available bool
	Version   string
	// ProbeError, when set, is the reason --version failed even though the
	// binary was on PATH. Surfaced in the picker so the user understands
	// why a "found but broken" install is greyed out.
	ProbeError string
	// InstallHint is a single command the user can run to install the
	// agent themselves. Surfaced in the picker for greyed-out entries.
	InstallHint string
}

// KnownAgents returns the static catalog. Availability is filled by
// DetectAgents.
func KnownAgents() []Agent {
	return []Agent{
		{
			ID:          AgentClaude,
			Label:       "Claude Code",
			Binary:      "claude",
			WpcTarget:   "claude",
			InstallHint: "curl -fsSL https://claude.ai/install.sh | bash   (macOS/Linux)  |  winget install Anthropic.ClaudeCode  (Windows)",
		},
		{
			// OpenClaude is a Claude Code fork that routes through any
			// OpenAI-compatible endpoint (Ollama, GPUStack, GitHub
			// Models, OAuth'd Codex, etc.) via the CLAUDE_CODE_USE_OPENAI
			// env switch. CLI surface, session-store layout
			// (~/.openclaude/projects/<slug>/<uuid>.jsonl), --continue /
			// --resume / --session-id flags, positional prompt arg,
			// CLAUDE.md auto-discovery — all inherited from upstream.
			// We compile via the same "claude" wpc target so SKILL.md +
			// CLAUDE.md scaffolding lands where openclaude already looks.
			ID:        AgentOpenClaude,
			Label:     "OpenClaude",
			Binary:    "openclaude",
			WpcTarget: "claude",
			// pnpm-only on purpose — npm has been the vector for the
			// recent supply-chain attacks (chalk/debug Sept 2025,
			// lottiefiles, etc.). pnpm + explicit registry pinning in
			// the installer narrows the trust surface. NOTE: openclaude
			// 0.13/0.14 has a phantom dep on @aws-sdk/client-bedrock-runtime
			// that crashes under strict pnpm at launch; until upstream
			// fixes it the user needs a hoisted global pnpm linker. See
			// the installer's openclaudePnpm comment.
			InstallHint: "pnpm add -g --registry=https://registry.npmjs.org/ @gitlawb/openclaude   (needs Node 20+ and pnpm; requires hoisted pnpm linker — see notes)",
		},
		{
			ID:          AgentCodex,
			Label:       "Codex CLI",
			Binary:      "codex",
			WpcTarget:   "codex",
			InstallHint: "pnpm add -g @openai/codex   (npm package; needs Node + pnpm)",
		},
		{
			ID:          AgentOpenCode,
			Label:       "OpenCode",
			Binary:      "opencode",
			WpcTarget:   "codex",
			InstallHint: "curl -fsSL https://opencode.ai/install | bash   |  pnpm add -g opencode-ai",
		},
		{
			ID:          AgentGemini,
			Label:       "Gemini CLI",
			Binary:      "gemini",
			WpcTarget:   "gemini",
			InstallHint: "pnpm add -g @google/gemini-cli   (npm package; needs Node 20+)",
		},
		{
			ID:    AgentDeepSeek,
			Label: "DeepSeek-TUI",
			// Upstream ships both `deepseek` (dispatcher) and
			// `deepseek-tui`; the dispatcher is what users invoke.
			Binary: "deepseek",
			// DeepSeek-TUI auto-loads AGENTS.md at session start (its
			// `init` subcommand creates one as a starter file), with
			// .claude/skills/ as a fallback discovery path. We
			// compile via the codex target so AGENTS.md lands at the
			// sandbox root and the agent picks up the workpath in its
			// system prompt from turn 1.
			WpcTarget: "codex",
			// pnpm-only hint — see comment on AgentOpenClaude above for
			// the supply-chain rationale.
			InstallHint: "pnpm add -g deepseek-tui   |   brew tap Hmbown/deepseek-tui && brew install deepseek-tui   (macOS)   |   scoop install deepseek-tui   (Windows)",
		},
	}
}

// DetectAgents probes a prioritized list of candidate paths for each
// agent and picks the first one whose `--version` actually exits 0.
// The order is:
//
//   1. exec.LookPath (whatever's on PATH first)
//   2. known per-agent install dirs (~/.opencode/bin, ~/.claude/local, ...)
//
// Trying every candidate matters because a broken binary can be on PATH
// while a working one sits in a known install dir — the real user case:
// pnpm's opencode.ps1 shim on %PATH% wraps a Windows-incompatible binary,
// but the official curl installer dropped a working native binary at
// ~/.opencode/bin/opencode.exe. We pick the second one.
//
// On success, Agent.Binary is rewritten to the absolute path of the
// resolved binary so the launcher can exec it directly even if the dir
// isn't on PATH in our process.
func DetectAgents(ctx context.Context) []Agent {
	agents := KnownAgents()
	for i := range agents {
		candidates := candidatePaths(agents[i].ID, agents[i].Binary)
		var lastErr error
		for _, candidate := range candidates {
			if st, err := os.Stat(candidate); err != nil || st.IsDir() {
				continue
			}
			version, perr := probeVersion(ctx, candidate)
			if perr != nil {
				if lastErr == nil {
					lastErr = perr
				}
				continue
			}
			agents[i].Available = true
			agents[i].Version = version
			agents[i].Binary = candidate
			break
		}
		if !agents[i].Available && lastErr != nil {
			agents[i].ProbeError = trimErr(lastErr)
		}
	}
	return agents
}

// candidatePaths returns the ordered list of full paths to try when
// detecting an agent: PATH first, then known install dirs as fallback.
func candidatePaths(id AgentID, binary string) []string {
	var paths []string
	if p, err := exec.LookPath(binary); err == nil {
		paths = append(paths, p)
	}
	for _, p := range knownInstallPaths(id, binary) {
		paths = append(paths, p)
	}
	return paths
}

// knownInstallPaths returns common per-agent locations to probe when
// exec.LookPath returns nothing. The official install scripts for these
// agents update the user shell rc; a Windows-native Clade
// process never sees that PATH change.
func knownInstallPaths(id AgentID, binary string) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	bins := []string{binary}
	if runtime.GOOS == "windows" {
		bins = append(bins, binary+".exe", binary+".cmd", binary+".bat")
	}

	var dirs []string
	switch id {
	case AgentOpenCode:
		// opencode.ai/install drops the binary here on every OS.
		dirs = append(dirs, filepath.Join(home, ".opencode", "bin"))
	case AgentClaude:
		dirs = append(dirs,
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, ".local", "bin"),
		)
	case AgentCodex:
		// npm/pnpm globals — covered by ImportPnpmPathIfPresent at
		// startup, but keep an explicit fallback for npm's location.
		if runtime.GOOS == "windows" {
			if appdata := os.Getenv("APPDATA"); appdata != "" {
				dirs = append(dirs, filepath.Join(appdata, "npm"))
			}
		}
	case AgentOpenClaude:
		// OpenClaude installs into a Clade-managed prefix (hoisted
		// node-linker) to dodge its phantom @aws-sdk dependency — its
		// bin lives at <prefix>/node_modules/.bin/openclaude, NOT on
		// the global pnpm path. Probe there first.
		if binDir, err := installer.ManagedAgentBinDir("openclaude"); err == nil {
			dirs = append(dirs, binDir)
		}
		// Fallback: a stray global install (pnpm or npm) from a user
		// who installed by hand. ImportPnpmPathIfPresent covers the
		// pnpm-global PATH case; mirror codex's Windows-npm fallback.
		if runtime.GOOS == "windows" {
			if appdata := os.Getenv("APPDATA"); appdata != "" {
				dirs = append(dirs, filepath.Join(appdata, "npm"))
			}
		}
	}

	var paths []string
	for _, d := range dirs {
		for _, b := range bins {
			paths = append(paths, filepath.Join(d, b))
		}
	}
	return paths
}

// probeVersion runs `<bin> --version` with a generous timeout. Returns
// the version line and a non-nil error if the binary couldn't produce
// a clean exit — caller uses that to decide whether the install is
// actually usable.
//
// Timeout note: bumped to 8s. The previous 3s killed Node-based agents
// on Windows (opencode, codex, deepseek-tui) where the first --version
// invocation per shell takes 3-6s due to Node startup + first-run
// telemetry/cache priming. The verdict became "broken install" on a
// perfectly working binary. 8s leaves headroom for cold-start while
// still failing fast on hung binaries.
//
// When the deadline IS hit, the error string is reshaped from the raw
// "signal: killed" (which reads as "the binary crashed") to an
// explicit "--version timed out after 8s" so the install screen
// surfaces actionable text.
func probeVersion(parent context.Context, path string) (string, error) {
	const deadline = 8 * time.Second
	ctx, cancel := context.WithTimeout(parent, deadline)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("--version timed out after %s (binary is slow to start, "+
				"not necessarily broken — try invoking it directly to confirm)", deadline)
		}
		return "", err
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return strings.TrimSpace(line), nil
}

// trimErr keeps Windows error blobs short enough to render in the picker.
func trimErr(err error) string {
	s := err.Error()
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
