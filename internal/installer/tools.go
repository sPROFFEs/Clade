package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ToolID names a non-agent capability Clade can install — a CLI the
// agent calls but the user doesn't launch as a primary session. Lives
// alongside AgentID so the installer can offer install/update for both
// without polluting the agent picker.
type ToolID string

const (
	// ToolGraphify is the knowledge-graph builder for codebases / docs.
	// Installed into a Clade-managed prefix via `uv tool install` so the
	// install is isolated from the user's Python environment.
	ToolGraphify ToolID = "graphify"
)

// Tool describes an installable non-agent capability. Same shape as
// Agent so the installer UI can render either with one code path; only
// the catalog (ToolMethods vs Methods) differs.
type Tool struct {
	ID          ToolID
	Label       string
	Binary      string
	InstallHint string
	Available   bool
	Version     string
	ProbeError  string
}

// KnownTools returns the static tool catalog. Availability is filled by
// DetectTools.
func KnownTools() []Tool {
	return []Tool{
		{
			ID:          ToolGraphify,
			Label:       "Graphify (knowledge-graph builder)",
			Binary:      "graphify",
			InstallHint: "uv tool install graphifyy   (needs uv on PATH; Clade installs into a managed prefix)",
		},
	}
}

// DetectTools mirrors DetectAgents: probes each known tool by trying its
// binary on PATH AND its Clade-managed prefix, taking the first that
// produces a clean --version exit.
func DetectTools(ctx context.Context) []Tool {
	tools := KnownTools()
	for i := range tools {
		candidates := toolCandidatePaths(tools[i].ID, tools[i].Binary)
		var lastErr error
		for _, cand := range candidates {
			if st, err := os.Stat(cand); err != nil || st.IsDir() {
				continue
			}
			version, perr := probeToolVersion(ctx, cand)
			if perr != nil {
				if lastErr == nil {
					lastErr = perr
				}
				continue
			}
			tools[i].Available = true
			tools[i].Version = version
			tools[i].Binary = cand
			break
		}
		if !tools[i].Available && lastErr != nil {
			tools[i].ProbeError = lastErr.Error()
		}
	}
	return tools
}

func toolCandidatePaths(id ToolID, binary string) []string {
	var paths []string
	// PATH first, in case the user installed by hand.
	if p, err := exec.LookPath(binary); err == nil {
		paths = append(paths, p)
	}
	// Clade-managed prefix second.
	if binDir, err := ManagedToolBinDir(string(id)); err == nil {
		bins := []string{binary}
		if runtime.GOOS == "windows" {
			bins = append(bins, binary+".exe", binary+".cmd", binary+".bat")
		}
		for _, b := range bins {
			paths = append(paths, filepath.Join(binDir, b))
		}
	}
	return paths
}

// probeToolVersion matches probeVersion in internal/launcher/agents.go:
// 8s deadline (Node / uv tools on Windows can take 3-6s to cold-start),
// with the timeout-vs-real-failure distinction surfaced as a readable
// error instead of the bare "signal: killed".
func probeToolVersion(parent context.Context, path string) (string, error) {
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

// ManagedToolPrefix returns the Clade-owned directory where a managed
// tool lives, alongside Clade's own config under
// os.UserConfigDir()/clade/tools/<name>/. Parallel to ManagedAgentPrefix
// but a separate subtree so an agent named "graphify" couldn't ever
// collide with a tool named "graphify".
func ManagedToolPrefix(name string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "clade", "tools", name), nil
}

// ManagedToolBinDir returns <prefix>/bin — the dir uv writes the
// tool's entrypoint shim into when invoked with UV_TOOL_BIN_DIR set
// to that path. Probed by tool detection.
func ManagedToolBinDir(name string) (string, error) {
	prefix, err := ManagedToolPrefix(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(prefix, "bin"), nil
}

// ToolMethods returns the install/update methods for a tool. Mirrors
// Methods (the AgentID variant) but with a tool-only catalog.
func ToolMethods(tool ToolID, action Action, current OS) []Method {
	all := allToolMethods(tool, action, current)
	var filtered []Method
	for _, m := range all {
		if !methodAvailable(m) {
			continue
		}
		filtered = append(filtered, m)
	}
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

func allToolMethods(tool ToolID, action Action, current OS) []Method {
	switch tool {
	case ToolGraphify:
		// uv tool install graphifyy — pure Python wheels, no native
		// deps, no postinstall scripts (unlike the openclaude case).
		// Installed into a Clade-managed prefix via UV_TOOL_DIR /
		// UV_TOOL_BIN_DIR so the user's global PATH stays unaffected
		// and the install is fully isolated from any system Python.
		//
		// --index-url is the supply-chain pin equivalent of pnpm's
		// --registry: defends against a poisoned ~/.config/uv/uv.toml
		// or UV_INDEX_URL env var.
		const pin = "graphifyy" // version-pin TODO: switch to graphifyy==X.Y.Z once vetted
		cmd := "uv tool install --index-url=https://pypi.org/simple/ " + pin
		if action == ActionUpdate {
			cmd = "uv tool upgrade --index-url=https://pypi.org/simple/ " + pin
		}
		return []Method{
			{
				ID:               "uv",
				Label:            "uv tool install into Clade-managed prefix",
				Command:          cmd,
				ManagedPrefix:    "graphify",
				ManagedPrefixPkg: pin,
				Recommended:      true,
				Prereqs:          []string{"uv"},
			},
		}
	}
	return nil
}

// EnsureUvReady probes uv on PATH. If missing, returns a clear error
// pointing the user at uv's installer — unlike pnpm we do NOT auto-
// install uv (no analog of corepack, and uv has its own supply-chain
// footprint the user should opt into deliberately).
//
// Returns the env additions (empty for uv today — it doesn't need a
// PNPM_HOME-style PATH injection because uv is one statically-linked
// binary) so callers can chain consistently with EnsurePnpmReady.
func EnsureUvReady(ctx context.Context, w io.Writer) ([]string, error) {
	if _, err := exec.LookPath("uv"); err == nil {
		return nil, nil
	}
	hint := "install uv first — it's a single static binary, no Python needed:\n"
	switch runtime.GOOS {
	case "windows":
		hint += "  irm https://astral.sh/uv/install.ps1 | iex\n" +
			"or:\n" +
			"  winget install --id=astral-sh.uv -e"
	case "darwin":
		hint += "  curl -LsSf https://astral.sh/uv/install.sh | sh\n" +
			"or:\n" +
			"  brew install uv"
	default:
		hint += "  curl -LsSf https://astral.sh/uv/install.sh | sh\n" +
			"(installs to ~/.local/bin; ensure that dir is on PATH)"
	}
	return nil, fmt.Errorf("uv not on PATH.\n%s", hint)
}

// ImportClademToolsToPath prepends every existing managed-tool bin dir
// to PATH so child processes (the launched agent + its tool wrappers)
// can find graphify-and-friends without the user editing their shell
// rc. Mirrors ImportPnpmPathIfPresent. No-op when the tools dir
// doesn't exist yet (first-run, no tools installed).
//
// Safe to call repeatedly; existing entries on PATH are not duplicated.
func ImportClademToolsToPath() {
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	toolsDir := filepath.Join(base, "clade", "tools")
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		return
	}
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		binDir := filepath.Join(toolsDir, e.Name(), "bin")
		if _, err := os.Stat(binDir); err != nil {
			continue
		}
		path := os.Getenv("PATH")
		cmp := func(a, b string) bool { return a == b }
		if runtime.GOOS == "windows" {
			cmp = strings.EqualFold
		}
		dup := false
		for _, entry := range strings.Split(path, sep) {
			if cmp(strings.TrimRight(entry, `\/`), strings.TrimRight(binDir, `\/`)) {
				dup = true
				break
			}
		}
		if !dup {
			_ = os.Setenv("PATH", binDir+sep+path)
		}
	}
}

// installUvIntoManagedPrefix runs `uv tool install <pkg>` with
// UV_TOOL_DIR / UV_TOOL_BIN_DIR pointed at the Clade-managed prefix so
// the binary lands at <prefix>/bin/<m.ManagedPrefix> and uv's package
// state stays in <prefix>/tools/. Mirrors the pnpm managed-prefix
// flow's UX (progress to stdout, success criterion = bin exists).
func installUvIntoManagedPrefix(ctx context.Context, m Method, extraEnv []string, stdout, stderr io.Writer) error {
	prefix, err := ManagedToolPrefix(m.ManagedPrefix)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return fmt.Errorf("create managed tool prefix %s: %w", prefix, err)
	}
	binDir := filepath.Join(prefix, "bin")
	toolDir := filepath.Join(prefix, "tools")
	for _, d := range []string{binDir, toolDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	fmt.Fprintf(stdout, "→ installing %s into Clade-managed prefix: %s\n", m.ManagedPrefixPkg, prefix)

	// uv reads UV_TOOL_DIR + UV_TOOL_BIN_DIR for the install destination.
	// We pass them via the command env so the user's global uv config is
	// untouched. --index-url is on the command line as a supply-chain
	// pin (also redundantly via UV_INDEX_URL just in case).
	parts := strings.Fields(m.Command)
	if len(parts) == 0 {
		return fmt.Errorf("empty uv command")
	}
	var cap bytes.Buffer
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	env := append(os.Environ(), extraEnv...)
	env = append(env,
		"UV_TOOL_DIR="+toolDir,
		"UV_TOOL_BIN_DIR="+binDir,
		"UV_INDEX_URL=https://pypi.org/simple/",
	)
	cmd.Env = env
	cmd.Stdout = io.MultiWriter(stdout, &cap)
	cmd.Stderr = io.MultiWriter(stderr, &cap)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s into %s: %w\n%s", m.Command, prefix, err, cap.String())
	}

	// Success criterion: the bin exists.
	bin := filepath.Join(binDir, m.ManagedPrefix)
	binPresent := false
	for _, cand := range []string{bin, bin + ".exe", bin + ".cmd", bin + ".bat"} {
		if _, err := os.Stat(cand); err == nil {
			binPresent = true
			bin = cand
			break
		}
	}
	if !binPresent {
		return fmt.Errorf("%s ran but %s binary not found under %s",
			m.Command, m.ManagedPrefix, binDir)
	}
	fmt.Fprintf(stdout, "✓ %s installed; binary at %s\n", m.ManagedPrefix, bin)
	return nil
}
