// clade is the TUI launcher for agent CLIs (Claude Code, Codex CLI,
// OpenCode) layered on top of wpc workpaths.
//
// Usage:
//
//	clade                  (interactive)
//	clade -version
//	clade -check-update    (report whether a newer release exists)
//	clade -update          (download + install the latest release)
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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/installer"
	"github.com/sPROFFEs/Clade/internal/launcher"
	"github.com/sPROFFEs/Clade/internal/ollama"
	"github.com/sPROFFEs/Clade/internal/updater"
	"github.com/sPROFFEs/Clade/internal/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	noSplash := flag.Bool("no-splash", false, "skip the boot animation")
	checkUpdateFlag := flag.Bool("check-update", false, "check GitHub for a newer release and exit")
	updateFlag := flag.Bool("update", false, "download and install the latest release, then exit")
	yesFlag := flag.Bool("y", false, "auto-confirm the update prompt (use with -update for non-interactive installs)")
	flag.Parse()
	if *versionFlag {
		fmt.Println(version.Banner)
		fmt.Printf("\nclade %s %s/%s\n", version.Current, runtime.GOOS, runtime.GOARCH)
		return
	}
	if *checkUpdateFlag {
		os.Exit(runCheckUpdate())
	}
	if *updateFlag {
		os.Exit(runUpdate(*yesFlag))
	}
	// screen_splash.go reads this to decide whether to show the
	// reveal animation. Also disabled when CLADE_NO_SPLASH=1 or
	// stdout isn't a terminal.
	noSplashFlag = *noSplash

	// If pnpm setup ran in a prior session, the user-level env vars it
	// wrote (registry on Windows, shell rc on Unix) won't have propagated
	// to the shell that spawned us. Eagerly add the pnpm bin dir to PATH
	// when it exists on disk so agent detection finds tools installed in
	// past sessions.
	installer.ImportPnpmPathIfPresent()

	// One-shot config migrations. Codex 0.40+ hard-errors on
	// wire_api="chat" — rewrite our managed block to "responses" so
	// existing users don't have to manually edit ~/.codex/config.toml.
	_, _ = ollama.MigrateCodexWireAPI()

	cfg, err := launcher.LoadConfig()
	if err != nil {
		die(err)
	}

	var firstScreen tea.Model
	if cfg == nil {
		firstScreen = newFirstRun()
	} else {
		firstScreen = newLayoutModel(cfg)
	}

	// Wrap the first screen in the boot splash unless we've opted
	// out (--no-splash, env var, non-TTY).
	var initial tea.Model = firstScreen
	if splashEnabled() {
		initial = newSplashModel(firstScreen)
	}

	root := &rootModel{screen: initial, cfg: cfg}
	// WithAltScreen takes over the terminal and restores the previous
	// content on exit — matches what professional TUIs (lazygit, k9s,
	// htop) do and keeps the user's shell scrollback clean.
	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		die(err)
	}
	// Stay-in-Clade model: agent runs are handled INSIDE the Bubbletea
	// program via tea.ExecProcess (see rootModel.Update). main() only
	// needs to bubble fatal errors back to the shell.
	if root.fatal != nil {
		die(root.fatal)
	}
}

// rootModel keeps the active screen and Clade-global state (the loaded
// config, last fatal error). Agent launches are handled inline via
// tea.ExecProcess in Update — the program no longer quits on launch.
type rootModel struct {
	screen tea.Model
	cfg    *launcher.Config
	fatal  error
}

// agentExitedMsg is dispatched by the tea.ExecProcess callback after the
// agent CLI returns. Carries everything the post-exit pipeline needs +
// the next screen to route to.
type agentExitedMsg struct {
	exitErr  error
	exitCode int
	cfg      *launcher.Config
}

func (m *rootModel) Init() tea.Cmd { return m.screen.Init() }

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Capture terminal size so chrome.go can compute body widths.
		// We still forward the message to the active screen in case it
		// wants to react too.
		setTermSize(msg.Width, msg.Height)
		// fall through to forward to inner screen below
	case tea.KeyMsg:
		// Global escape hatches.
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		}
	case screenDoneMsg:
		if msg.launch != nil {
			// Persist cfg early so a SIGKILL mid-agent doesn't lose
			// the LastAgent / WorkspacesRoot updates.
			if msg.updateCfg != nil {
				_ = launcher.SaveConfig(msg.updateCfg)
				m.cfg = msg.updateCfg
			}
			cmd := buildAgentCmd(*msg.launch)
			ws := msg.launchedWS
			agentID := msg.launchedAgent
			cfg := m.cfg
			sessionStart := launcher.LastSessionStartedAt
			if sessionStart.IsZero() {
				sessionStart = time.Now().UTC()
			}
			// tea.ExecProcess hands the TTY to the agent, runs it,
			// then calls the callback. We return its result message
			// (agentExitedMsg) which Update handles below — staying
			// in Clade, redrawing the chat list.
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				sessionEnd := time.Now().UTC()
				if ws != nil {
					_ = launcher.SyncMemoryBack(*ws)
				}
				if ws != nil && agentID != "" {
					if agent, ok := launcher.ResolveAgentForChat(agentID, 2*time.Second); ok {
						_, _ = launcher.CapturePostExit(*ws, agent, sessionStart, sessionEnd)
					}
				}
				return agentExitedMsg{
					exitErr:  err,
					exitCode: extractExitCode(err),
					cfg:      cfg,
				}
			})
		}
		if msg.next != nil {
			// Pre-launcher screens (splash, firstRun) don't intercept
			// screenDoneMsg themselves — they ask root to swap the
			// whole tree (typically to the layoutModel). Once
			// layoutModel is in charge, the swap is its responsibility
			// (it changes the active pane, not the entire tree).
			if _, isLayout := m.screen.(*layoutModel); !isLayout {
				m.screen = msg.next
				return m, m.screen.Init()
			}
			// fall through to the inner Update so layoutModel handles it
		}
	case agentExitedMsg:
		// Agent finished. Land back on the chat list so the user can
		// keep working in Clade. Diagnostics on the chat list will
		// pick up the just-captured session via LoadRecentSummaries.
		cfg := msg.cfg
		if cfg == nil {
			cfg = m.cfg
		}
		m.screen = newLayoutModel(cfg)
		return m, m.screen.Init()
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

// buildAgentCmd assembles the *exec.Cmd for tea.ExecProcess. Bubbletea
// itself wires stdin/stdout/stderr to the host terminal during execution
// (releasing the alt screen, restoring it on exit), so we only set
// command/args/dir/env here.
func buildAgentCmd(plan launcher.LaunchPlan) *exec.Cmd {
	cmd := exec.Command(plan.Command, plan.Args...)
	cmd.Dir = plan.Dir
	if len(plan.Env) > 0 {
		env := os.Environ()
		for k, v := range plan.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	return cmd
}

// extractExitCode pulls the OS exit code out of cmd.Run-style errors.
// Bubbletea's ExecProcess callback gets the same error shape exec.Cmd
// would return synchronously. A nil error means exit 0.
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "clade: %v\n", err)
	os.Exit(1)
}

// runCheckUpdate prints whether a newer release exists on GitHub. Used
// by the -check-update flag; safe to run in scripts (no side effects,
// no prompts).
func runCheckUpdate() int {
	rel, err := updater.FetchLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clade: %v\n", err)
		return 1
	}
	if updater.IsNewer(rel.TagName, version.Current) {
		fmt.Printf("update available: %s (currently %s)\n  %s\n", rel.TagName, version.Current, rel.HTMLURL)
		fmt.Println("\nRun `clade -update` to install it.")
		return 0
	}
	fmt.Printf("up to date (%s is the latest release)\n", version.Current)
	return 0
}

// runUpdate fetches the latest release, prompts the user (unless -y is
// set), and swaps the clade binary in place. Exits 0 on success,
// non-zero on any failure or user decline.
func runUpdate(autoYes bool) int {
	rel, err := updater.FetchLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clade: %v\n", err)
		return 1
	}
	if !updater.IsNewer(rel.TagName, version.Current) {
		fmt.Printf("already on the latest release (%s)\n", version.Current)
		return 0
	}
	asset, err := updater.AssetForHost(rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clade: %v\n", err)
		return 1
	}

	fmt.Printf("Update available: %s → %s\n", version.Current, rel.TagName)
	fmt.Printf("Asset: %s (%.1f MB)\n", asset.Name, float64(asset.Size)/(1024*1024))
	fmt.Printf("Release notes: %s\n\n", rel.HTMLURL)

	if !autoYes {
		fmt.Print("Install now? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" && answer != "yes" {
			fmt.Println("cancelled")
			return 1
		}
	}

	err = updater.Apply(asset, func(stage string) {
		fmt.Println("  …", stage)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "clade: update failed: %v\n", err)
		return 1
	}
	fmt.Printf("\n✓ installed %s. Re-run `clade` to start the new version.\n", rel.TagName)
	return 0
}
