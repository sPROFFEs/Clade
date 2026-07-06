package main

// `praimate code [args...]` — run the bundled PrAImate Code CLI, our
// version-pinned, rebranded build of OpenCode (MIT). Like `--gui`, the
// heavy artifact ships as a sibling binary (praimate-code) rather than
// inflating the lean TUI binary; this dispatcher locates it and execs
// it with the user's args, forwarding stdio so the interactive coding
// session works normally.
//
// Resolution order:
//  1. praimate-code(.exe) next to this executable (release archives
//     ship them side by side)
//  2. praimate-code on PATH
//  3. <config>/praimate/bin/praimate-code (managed-install location)

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/store"
)

func codeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "praimate-code.exe"
	}
	return "praimate-code"
}

// resolveCodeBinary returns the path to praimate-code, or "" if not
// found. Split out for testability.
func resolveCodeBinary(exePath, configDir string) string {
	name := codeBinaryName()
	candidates := []string{}
	if exePath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), name))
	}
	if configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "praimate", "bin", name))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// runCode locates praimate-code and runs it with args, returning its
// exit code for main() to pass through.
func runCode(args []string) int {
	exePath, _ := os.Executable()
	exePath, _ = filepath.EvalSymlinks(exePath)
	configDir, _ := os.UserConfigDir()

	bin := resolveCodeBinary(exePath, configDir)
	if bin == "" {
		fmt.Fprintln(os.Stderr, "praimate: praimate-code not found next to this binary or on PATH.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "PrAImate Code is our version-pinned build of OpenCode. It ships")
		fmt.Fprintln(os.Stderr, "prebuilt in the linux-amd64 and windows-amd64 release archives.")
		fmt.Fprintln(os.Stderr, "Build it from source with:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  scripts/build-praimate-code.sh")
		return 127
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), prepareCodeMCPEnv()...)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "praimate: failed to run praimate-code: %v\n", err)
		return 1
	}
	return 0
}

func prepareCodeMCPEnv() []string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return nil
	}
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil
	}
	defer st.Close()
	c, err := core.New(core.Options{Store: st})
	if err != nil {
		return nil
	}
	env, err := c.PrepareEnabledMCPForRun(context.Background(), "praimate-code", cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "praimate: MCP config warning: %v\n", err)
		return nil
	}
	return envMapList(env)
}

func envMapList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}
