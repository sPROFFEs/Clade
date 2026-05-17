package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sdksdk/wpc/pkg/targets"
	"github.com/sdksdk/wpc/pkg/workpath"
)

// PrepareSandbox loads the workpath, validates it, and compiles it into
// the workspace sandbox using the agent's matching wpc target. After this
// returns, the sandbox is ready for the agent to be spawned with that as
// its cwd. We never write into the workpath source — that stays read-only
// as far as the launcher is concerned.
func PrepareSandbox(ws Workspace, agent Agent) error {
	if !agent.Available {
		return fmt.Errorf("agent %s is not installed", agent.ID)
	}

	if err := EnsureSandbox(ws); err != nil {
		return err
	}

	wp, err := workpath.LoadDir(ws.WorkpathDir)
	if err != nil {
		return fmt.Errorf("load workpath: %w", err)
	}
	// The wpc loader defaults Name to the directory's basename, which is
	// always literally "workpath" under our layout. Override so the
	// compiled output is identified by the workspace name (matters for
	// Claude skill dirs, mika modules, and the generic <name>.md target).
	wp.Name = ws.Name
	if err := workpath.Validate(wp); err != nil {
		return fmt.Errorf("validate workpath: %w", err)
	}

	t, err := targets.Get(agent.WpcTarget)
	if err != nil {
		return err
	}
	if err := t.Compile(wp, ws.SandboxDir); err != nil {
		return fmt.Errorf("compile %s into sandbox: %w", agent.WpcTarget, err)
	}

	return writeSandboxReadme(ws, agent)
}

// writeSandboxReadme drops an orientation file on first compile so the
// user lands in something self-explanatory when they cd into the sandbox.
func writeSandboxReadme(ws Workspace, agent Agent) error {
	path := filepath.Join(ws.SandboxDir, "WAIFU_SANDBOX.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	body := fmt.Sprintf(`# Sandbox for %s

Knowledge base: %s
Agent: %s (binary: %s)

This directory is the agent's working directory. The launcher compiled the
workpath into it using the %q wpc target, so the agent loads the mission,
playbook, rules, and any tools/subagents on startup.

Anything you create here is gitignored by default. Treat the parent
workpath (../workpath/) as read-only — edit it via the launcher (or by
hand) and re-run the launcher to recompile.
`, ws.Name, ws.WorkpathDir, agent.Label, agent.Binary, agent.WpcTarget)
	return os.WriteFile(path, []byte(body), 0o644)
}

// LaunchPlan describes what the TUI hands off to the OS once Bubble Tea
// has released the terminal: which binary to exec, with what args, in
// which directory.
type LaunchPlan struct {
	Command string
	Args    []string
	Dir     string
}

// Plan builds the LaunchPlan but does not actually exec. The TUI layer
// must run Plan first (so the workpath is compiled and any error
// surfaces inside the UI), then quit the Bubble Tea program, then exec
// the plan from main() while the terminal is no longer being driven.
func Plan(ws Workspace, agent Agent) (LaunchPlan, error) {
	if err := PrepareSandbox(ws, agent); err != nil {
		return LaunchPlan{}, err
	}
	return LaunchPlan{
		Command: agent.Binary,
		Args:    nil,
		Dir:     ws.SandboxDir,
	}, nil
}
