// code-launcher is the TUI launcher for agent CLIs (Claude Code, Codex CLI,
// OpenCode) layered on top of wpc workpaths.
//
// Usage:
//
//	code-launcher                  (interactive)
//	code-launcher -version
//
// The launcher detects first run (no config), walks the user through
// picking a workspaces root, lists / creates workspaces, detects which
// agent CLIs are installed, compiles the chosen workpath into the
// workspace's sandbox using the right wpc target, then hands the TTY off
// to the agent.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/installer"
	"github.com/sdksdk/code-launcher/internal/launcher"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("code-launcher 0.1.0 %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	// If pnpm setup ran in a prior session, the user-level env vars it
	// wrote (registry on Windows, shell rc on Unix) won't have propagated
	// to the shell that spawned us. Eagerly add the pnpm bin dir to PATH
	// when it exists on disk so agent detection finds tools installed in
	// past sessions.
	installer.ImportPnpmPathIfPresent()

	cfg, err := launcher.LoadConfig()
	if err != nil {
		die(err)
	}

	var initial tea.Model
	if cfg == nil {
		initial = newFirstRun()
	} else {
		initial = newWorkspacesModel(cfg)
	}

	root := &rootModel{screen: initial}
	prog := tea.NewProgram(root)
	if _, err := prog.Run(); err != nil {
		die(err)
	}

	if root.launch != nil {
		// Persist the choice of agent for next session.
		if root.updateCfg != nil {
			_ = launcher.SaveConfig(root.updateCfg)
		}
		code := execAgent(*root.launch)
		// Sync MEMORY.md back from the sandbox so the agent's writes
		// persist across launches. Best-effort — failures don't change
		// the exit code.
		if root.launchedWS != nil {
			_ = launcher.SyncMemoryBack(*root.launchedWS)
		}
		os.Exit(code)
	}
	if root.fatal != nil {
		die(root.fatal)
	}
}

// rootModel keeps the active screen, plus two pieces of "result state" the
// outer main() needs after Bubble Tea has finished: the launch plan and a
// fatal error (if any).
type rootModel struct {
	screen     tea.Model
	launch     *launcher.LaunchPlan
	updateCfg  *launcher.Config
	launchedWS *launcher.Workspace // remembered for post-launch sync-back
	fatal      error
}

func (m *rootModel) Init() tea.Cmd { return m.screen.Init() }

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global escape hatches.
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		}
	case screenDoneMsg:
		if msg.launch != nil {
			m.launch = msg.launch
			m.updateCfg = msg.updateCfg
			m.launchedWS = msg.launchedWS
			return m, tea.Quit
		}
		if msg.next != nil {
			m.screen = msg.next
			return m, m.screen.Init()
		}
	case errMsg:
		// Bubble unrecoverable errors up to main() — most screen-level
		// errors are caught inside the screen and surfaced inline.
		m.fatal = msg.err
		return m, tea.Quit
	}
	next, cmd := m.screen.Update(msg)
	m.screen = next
	return m, cmd
}

func (m *rootModel) View() string { return m.screen.View() }

// execAgent spawns the agent CLI with stdio inherited from this process,
// in the workspace's sandbox dir. Bubble Tea has already released the
// terminal (program.Run returned), so the agent owns the TTY cleanly.
//
// We never use syscall.Exec, even on Unix: keeping a parent process means
// the agent's exit code propagates back through our Exit, and we get one
// uniform path on every OS.
func execAgent(plan launcher.LaunchPlan) int {
	cmd := exec.Command(plan.Command, plan.Args...)
	cmd.Dir = plan.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(plan.Env) > 0 {
		env := os.Environ()
		for k, v := range plan.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "code-launcher: launch %s: %v\n", plan.Command, err)
		return 1
	}
	return 0
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "code-launcher: %v\n", err)
	os.Exit(1)
}
