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
	"strconv"
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
	// ToolGstack is Garry Tan's skill pack. It is not a normal binary
	// helper: setup installs slash-command skills into supported hosts
	// (Claude Code, Codex, OpenCode, etc.). Clade keeps the git checkout
	// under clade/tools/gstack and runs upstream setup from there.
	ToolGstack ToolID = "gstack"
	// ToolScrapeGraph installs a tiny Clade wrapper around ScrapeGraphAI's
	// Python packages so every agent can call `scrapegraph-search`.
	ToolScrapeGraph ToolID = "scrapegraph"
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
		{
			ID:          ToolGstack,
			Label:       "gstack (AI workflow skill pack)",
			Binary:      "gstack",
			InstallHint: "git clone + ./setup --host auto   (needs git + bun; skills work in Claude/Codex/OpenCode/Gemini, not DeepSeek)",
		},
		{
			ID:          ToolScrapeGraph,
			Label:       "ScrapeGraphAI search helper",
			Binary:      "scrapegraph-search",
			InstallHint: "uv venv + scrapegraphai/scrapegraph-py   (set SGAI_API_KEY for API mode, or SCRAPEGRAPH_LLM_MODEL for OSS/local mode)",
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
	case ToolGstack:
		// gstack's upstream setup is a bash script that installs skills
		// into whichever supported hosts it detects. The README's broad
		// support list includes Claude Code, Codex, OpenCode and Gemini.
		// DeepSeek-TUI is not a gstack host today, so this stays a skill
		// pack tool rather than a generic runtime helper.
		//
		// GSTACK_SKIP_* prevents setup from making system package-manager
		// changes (fonts/coreutils). Clade should install the skill pack,
		// not mutate the host OS behind the user's back.
		return []Method{
			{
				ID:    "bash",
				Label: "git clone + gstack setup into detected supported hosts",
				Command: "git clone --single-branch --depth 1 https://github.com/garrytan/gstack.git <managed>/repo && " +
					"cd <managed>/repo && GSTACK_SKIP_FONTS=1 GSTACK_SKIP_COREUTILS=1 ./setup --host auto --no-team -q",
				ManagedPrefix:    "gstack",
				ManagedPrefixPkg: "https://github.com/garrytan/gstack.git",
				Recommended:      true,
				Prereqs:          []string{"git", "bun"},
			},
		}
	case ToolScrapeGraph:
		// scrapegraphai does not expose a stable console script we can
		// rely on. Install the packages into a Clade-owned venv and write
		// our own small `scrapegraph-search` wrapper. The wrapper supports
		// ScrapeGraphAI v2 API mode (SGAI_API_KEY) and OSS SearchGraph mode
		// (local/Ollama-capable via SCRAPEGRAPH_LLM_MODEL).
		const pkgs = "scrapegraphai scrapegraph-py>=2.1.0"
		return []Method{
			{
				ID:               "uv",
				Label:            "uv venv install + Clade scrapegraph-search wrapper",
				Command:          "uv venv <managed>/venv && uv pip install --python <managed>/venv/{bin,Scripts}/python --index-url=https://pypi.org/simple/ scrapegraphai 'scrapegraph-py>=2.1.0'",
				ManagedPrefix:    "scrapegraph",
				ManagedPrefixPkg: pkgs,
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

func installGstackIntoManagedPrefix(ctx context.Context, m Method, extraEnv []string, stdout, stderr io.Writer) error {
	prefix, err := ManagedToolPrefix(m.ManagedPrefix)
	if err != nil {
		return err
	}
	repo := filepath.Join(prefix, "repo")
	binDir := filepath.Join(prefix, "bin")
	for _, d := range []string{prefix, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		fmt.Fprintf(stdout, "→ updating gstack checkout: %s\n", repo)
		if err := runToolCommand(ctx, stdout, stderr, "", extraEnv,
			"git", "-C", repo, "pull", "--ff-only"); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "→ cloning gstack into Clade-managed prefix: %s\n", repo)
		if err := os.RemoveAll(repo); err != nil {
			return fmt.Errorf("clean stale gstack checkout %s: %w", repo, err)
		}
		if err := runToolCommand(ctx, stdout, stderr, "", extraEnv,
			"git", "clone", "--single-branch", "--depth", "1", m.ManagedPrefixPkg, repo); err != nil {
			return err
		}
	}

	setupEnv := append([]string{}, extraEnv...)
	setupEnv = append(setupEnv,
		"GSTACK_SKIP_FONTS=1",
		"GSTACK_SKIP_COREUTILS=1",
		"CI=1",
	)
	fmt.Fprintln(stdout, "→ running gstack setup for detected supported hosts...")
	if err := runToolCommand(ctx, stdout, stderr, repo, setupEnv,
		"bash", "./setup", "--host", "auto", "--no-team", "-q"); err != nil {
		return err
	}
	if err := writeGstackWrapper(binDir, repo); err != nil {
		return err
	}
	bin := filepath.Join(binDir, "gstack")
	if runtime.GOOS == "windows" {
		bin += ".cmd"
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("gstack setup ran but wrapper not found at %s: %w", bin, err)
	}
	fmt.Fprintf(stdout, "✓ gstack skills installed; detection wrapper at %s\n", bin)
	return nil
}

func installScrapeGraphIntoManagedVenv(ctx context.Context, m Method, extraEnv []string, stdout, stderr io.Writer) error {
	prefix, err := ManagedToolPrefix(m.ManagedPrefix)
	if err != nil {
		return err
	}
	venv := filepath.Join(prefix, "venv")
	binDir := filepath.Join(prefix, "bin")
	for _, d := range []string{prefix, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	fmt.Fprintf(stdout, "→ creating ScrapeGraphAI venv: %s\n", venv)
	if err := runToolCommand(ctx, stdout, stderr, "", extraEnv,
		"uv", "venv", venv); err != nil {
		return err
	}
	python := managedVenvPython(venv)
	installEnv := append([]string{}, extraEnv...)
	installEnv = append(installEnv, "UV_INDEX_URL=https://pypi.org/simple/")
	args := []string{
		"pip", "install",
		"--python", python,
		"--index-url=https://pypi.org/simple/",
	}
	args = append(args, strings.Fields(m.ManagedPrefixPkg)...)
	fmt.Fprintln(stdout, "→ installing scrapegraphai + scrapegraph-py into managed venv...")
	if err := runToolCommand(ctx, stdout, stderr, "", installEnv, "uv", args...); err != nil {
		return err
	}
	if err := writeScrapeGraphWrapper(prefix, binDir, python); err != nil {
		return err
	}
	bin := filepath.Join(binDir, "scrapegraph-search")
	if runtime.GOOS == "windows" {
		bin += ".cmd"
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("scrapegraph install ran but wrapper not found at %s: %w", bin, err)
	}
	fmt.Fprintf(stdout, "✓ ScrapeGraphAI installed; binary at %s\n", bin)
	return nil
}

func runToolCommand(ctx context.Context, stdout, stderr io.Writer, dir string, env []string, name string, args ...string) error {
	fmt.Fprintf(stdout, "$ %s\n", strings.Join(append([]string{name}, args...), " "))
	var cap bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = io.MultiWriter(stdout, &cap)
	cmd.Stderr = io.MultiWriter(stderr, &cap)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, cap.String())
	}
	return nil
}

func writeGstackWrapper(binDir, repo string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create gstack bin dir: %w", err)
	}
	version := "gstack installed (managed by Clade)"
	if raw, err := os.ReadFile(filepath.Join(repo, "package.json")); err == nil {
		if i := bytes.Index(raw, []byte(`"version"`)); i >= 0 {
			version = "gstack skill pack (managed by Clade)"
		}
	}
	unix := "#!/usr/bin/env sh\n" +
		"if [ \"${1:-}\" = \"--version\" ]; then\n" +
		"  printf '%s\\n' " + shellQuote(version) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' 'gstack is installed as AI-agent skills via Clade.'\n" +
		"printf '%s\\n' 'Use the gstack slash commands inside supported hosts: Claude Code, Codex, OpenCode, Gemini CLI.'\n" +
		"printf '%s\\n' 'DeepSeek-TUI is not a documented gstack host.'\n" +
		"printf 'source: %s\\n' " + shellQuote(repo) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "gstack"), []byte(unix), 0o755); err != nil {
		return fmt.Errorf("write gstack wrapper: %w", err)
	}
	cmd := "@echo off\r\n" +
		"if \"%~1\"==\"--version\" (\r\n" +
		"  echo " + version + "\r\n" +
		"  exit /b 0\r\n" +
		")\r\n" +
		"echo gstack is installed as AI-agent skills via Clade.\r\n" +
		"echo Use the gstack slash commands inside supported hosts: Claude Code, Codex, OpenCode, Gemini CLI.\r\n" +
		"echo DeepSeek-TUI is not a documented gstack host.\r\n" +
		"echo source: " + repo + "\r\n"
	if err := os.WriteFile(filepath.Join(binDir, "gstack.cmd"), []byte(cmd), 0o755); err != nil {
		return fmt.Errorf("write gstack Windows wrapper: %w", err)
	}
	return nil
}

func writeScrapeGraphWrapper(prefix, binDir, python string) error {
	scriptPath := filepath.Join(prefix, "scrapegraph-search.py")
	if err := os.WriteFile(scriptPath, []byte(scrapeGraphSearchPython()), 0o644); err != nil {
		return fmt.Errorf("write scrapegraph-search.py: %w", err)
	}
	unix := "#!/usr/bin/env sh\nexec " + shellQuote(python) + " " + shellQuote(scriptPath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "scrapegraph-search"), []byte(unix), 0o755); err != nil {
		return fmt.Errorf("write scrapegraph-search wrapper: %w", err)
	}
	cmd := "@echo off\r\n" + strconv.Quote(python) + " " + strconv.Quote(scriptPath) + " %*\r\n"
	if err := os.WriteFile(filepath.Join(binDir, "scrapegraph-search.cmd"), []byte(cmd), 0o755); err != nil {
		return fmt.Errorf("write scrapegraph-search Windows wrapper: %w", err)
	}
	return nil
}

func managedVenvPython(venv string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venv, "Scripts", "python.exe")
	}
	return filepath.Join(venv, "bin", "python")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func scrapeGraphSearchPython() string {
	return `#!/usr/bin/env python3
import argparse
import json
import os
import sys

VERSION = "scrapegraph-search (Clade wrapper for scrapegraphai + scrapegraph-py)"


def parse_args():
    p = argparse.ArgumentParser(
        prog="scrapegraph-search",
        description="Search/scrape helper backed by ScrapeGraphAI. Prints JSON."
    )
    p.add_argument("query", nargs="*", help="search query or URL task")
    p.add_argument("--prompt", help="explicit extraction prompt; defaults to the query")
    p.add_argument("--mode", choices=("auto", "api", "oss"), default=os.getenv("SCRAPEGRAPH_MODE", "auto"))
    p.add_argument("--results", type=int, default=int(os.getenv("SCRAPEGRAPH_RESULTS", "5")))
    p.add_argument("--version", action="store_true")
    return p.parse_args()


def json_default(value):
    try:
        return vars(value)
    except TypeError:
        return str(value)


def emit(payload):
    print(json.dumps(payload, ensure_ascii=False, indent=2, default=json_default))


def run_api(prompt, results):
    key = os.getenv("SGAI_API_KEY")
    if not key:
        raise RuntimeError("SGAI_API_KEY is required for ScrapeGraphAI API mode")
    try:
        from scrapegraph_py import Client
        client = Client(api_key=key)
        return client.search(query=prompt, num_results=results)
    except ImportError:
        from scrapegraph_py import ScrapeGraphAI
        client = ScrapeGraphAI(api_key=key)
        return client.search(query=prompt, num_results=results)


def run_oss(prompt):
    from scrapegraphai.graphs import SearchGraph

    model = os.getenv("SCRAPEGRAPH_LLM_MODEL", "ollama/llama3.2")
    tokens = int(os.getenv("SCRAPEGRAPH_MODEL_TOKENS", "8192"))
    config = {
        "llm": {
            "model": model,
            "model_tokens": tokens,
        },
        "embeddings": {
            "model": os.getenv("SCRAPEGRAPH_EMBEDDINGS_MODEL", "ollama/nomic-embed-text"),
        },
        "verbose": os.getenv("SCRAPEGRAPH_VERBOSE", "false").lower() == "true",
        "headless": True,
    }
    if os.getenv("SERPER_API_KEY"):
        config["search_engine"] = "serper"
    if os.getenv("SCRAPEGRAPH_SEARCH_ENGINE"):
        config["search_engine"] = os.getenv("SCRAPEGRAPH_SEARCH_ENGINE")
    os.environ.setdefault("SCRAPEGRAPHAI_TELEMETRY_ENABLED", "false")
    graph = SearchGraph(prompt=prompt, config=config)
    return graph.run()


def main():
    args = parse_args()
    if args.version:
        print(VERSION)
        return 0
    query = " ".join(args.query).strip()
    prompt = (args.prompt or query).strip()
    if not prompt:
        print("scrapegraph-search: query or --prompt is required", file=sys.stderr)
        return 2
    try:
        if args.mode == "api" or (args.mode == "auto" and os.getenv("SGAI_API_KEY")):
            emit({"mode": "api", "query": query or prompt, "result": run_api(prompt, args.results)})
        else:
            emit({"mode": "oss", "query": query or prompt, "result": run_oss(prompt)})
    except Exception as exc:
        print("scrapegraph-search failed: " + str(exc), file=sys.stderr)
        if args.mode == "auto" and not os.getenv("SGAI_API_KEY"):
            print("hint: set SGAI_API_KEY for ScrapeGraphAI API mode, or configure OSS mode with SCRAPEGRAPH_LLM_MODEL/SERPER_API_KEY", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
`
}
