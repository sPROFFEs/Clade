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
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/backup"
	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
	"github.com/sPROFFEs/PrAImate/internal/ollama"
	"github.com/sPROFFEs/PrAImate/internal/updater"
	"github.com/sPROFFEs/PrAImate/internal/version"
)

func main() {
	// `praimate code [args...]` dispatches to the bundled praimate-code
	// binary (our rebranded build of OpenCode). Handled before
	// flag.Parse so OpenCode's own flags pass straight through.
	if len(os.Args) >= 2 && os.Args[1] == "code" {
		os.Exit(runCode(os.Args[2:]))
	}

	versionFlag := flag.Bool("version", false, "print version and exit")
	guiFlag := flag.Bool("gui", false,
		"launch the praimate-gui desktop app and exit (looks next to this binary, then PATH)")
	noSplash := flag.Bool("no-splash", false, "skip the boot animation")
	checkUpdateFlag := flag.Bool("check-update", false, "check GitHub for a newer release and exit")
	updateFlag := flag.Bool("update", false, "download and install the latest release, then exit")
	yesFlag := flag.Bool("y", false, "auto-confirm the update prompt (use with -update for non-interactive installs)")
	installTool := flag.String("install-tool", "",
		"install a PrAImate-managed tool into <config>/praimate/tools/<name>/ and exit. "+
			"Currently supported: graphify, gstack, scrapegraph. Use this when a workpath imports a "+
			"_common/<bundle> whose wrapper scripts need a binary that's not yet "+
			"on PATH.")
	mergeMemory := flag.Bool("merge-memory", false,
		"git merge driver hook: concatenate %O %A %B and write result to %A. "+
			"Wired into .git/config by the backup feature; not for direct use.")
	runAgent := flag.String("run-agent", "",
		"run a built-in or imported agent's default workflow non-interactively "+
			"and print the assistant reply. Combine with -cli and -workflow as needed.")
	runCLI := flag.String("cli", "claude",
		"which third-party CLI to drive when running -run-agent (default: claude)")
	runWorkflow := flag.String("workflow", "",
		"workflow name to run with -run-agent (defaults to the agent's default_workflow)")
	runInputs := flag.String("inputs", "",
		"comma-separated key=value pairs for -run-agent workflow inputs (e.g. task=hello,tone=polite)")
	listAgents := flag.Bool("list-agents", false,
		"print every agent in the DB (built-in + imported) and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Println(version.Banner)
		fmt.Printf("\n%s %s %s/%s\n", version.Name, version.Current, runtime.GOOS, runtime.GOARCH)
		return
	}
	if *guiFlag {
		os.Exit(launchGUI())
	}
	if *mergeMemory {
		// Args left in flag.Args() are: <ancestor> <ours> <theirs>.
		// Git passes them as %O %A %B per the merge driver spec.
		os.Exit(runMergeMemory(flag.Args()))
	}
	if *checkUpdateFlag {
		os.Exit(runCheckUpdate())
	}
	if *updateFlag {
		os.Exit(runUpdate(*yesFlag))
	}
	if *installTool != "" {
		os.Exit(runInstallTool(*installTool))
	}
	if *listAgents {
		os.Exit(runListAgents())
	}
	if *runAgent != "" {
		os.Exit(runAgentWorkflow(*runAgent, *runCLI, *runWorkflow, *runInputs))
	}
	// screen_splash.go reads this to decide whether to show the
	// reveal animation. Also disabled when PRAIMATE_NO_SPLASH=1 or
	// stdout isn't a terminal.
	noSplashFlag = *noSplash

	// If pnpm setup ran in a prior session, the user-level env vars it
	// wrote (registry on Windows, shell rc on Unix) won't have propagated
	// to the shell that spawned us. Eagerly add the pnpm bin dir to PATH
	// when it exists on disk so agent detection finds tools installed in
	// past sessions.
	installer.ImportPnpmPathIfPresent()
	// Same idea for Clade-managed tool prefixes: prepend
	// each <config>/clade/tools/<name>/bin to PATH so wpc-staged template
	// scripts can call those binaries by name without knowing the prefix.
	installer.ImportClademToolsToPath()
	// And the managed standalone bin dir (<config>/praimate/bin) so
	// praimate-code is found by agent detection and `praimate code`.
	installer.ImportPraimateBinToPath()

	// One-shot config migrations. Codex 0.40+ hard-errors on
	// wire_api="chat" — rewrite our managed block to "responses" so
	// existing users don't have to manually edit ~/.codex/config.toml.
	_, _ = ollama.MigrateCodexWireAPI()

	cfg, err := launcher.LoadConfig()
	if err != nil {
		die(err)
	}

	// Open the PrAImate DB, seed built-in agents, register CLI
	// adapters. Failure is non-fatal — the legacy flows keep working
	// even if the new Recipes pane is unavailable; that pane surfaces
	// the error itself when visited.
	initAppCore()

	// Backup auto-sync on startup. Gated on the master switch + the
	// auto-sync sub-toggle + a configured remote. When the master
	// switch is OFF the feature is fully dormant and this branch is
	// a no-op — Clade behaves exactly as it did pre-0.1.11.
	if cfg != nil && cfg.BackupEnabled && cfg.BackupAutoSync && cfg.BackupRemoteURL != "" {
		runStartupAutoSync(cfg)
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
	// Backup auto-sync on exit. Same master-switch gate as startup.
	// Picks up the latest cfg in case the user toggled auto-sync
	// inside the session.
	if root.cfg != nil && root.cfg.BackupEnabled && root.cfg.BackupAutoSync && root.cfg.BackupRemoteURL != "" {
		runExitAutoSync(root.cfg)
	}

	// Stay-in-Clade model: agent runs are handled INSIDE the Bubbletea
	// program via tea.ExecProcess (see rootModel.Update). main() only
	// needs to bubble fatal errors back to the shell.
	if root.fatal != nil {
		die(root.fatal)
	}
}

// runStartupAutoSync fetches + applies safe sync actions before the
// TUI launches. Diverged repos halt and print a message telling the
// user to resolve via the Backup tab. Force-always-local skips the
// halt and force-pushes.
//
// Best-effort: any failure prints a one-liner and lets the launcher
// continue (we don't want a network blip to lock the user out of
// their local chats).
func runStartupAutoSync(cfg *launcher.Config) {
	dir := cfg.WorkspacesRoot
	if !backup.IsGitRepo(dir) {
		// First sync on a freshly-created empty/sample root — init
		// + push so subsequent runs have something to fetch.
		fmt.Fprintf(os.Stderr, "[backup] initialising %s as a git repo...\n", dir)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := backup.Init(ctx, dir); err != nil {
			fmt.Fprintf(os.Stderr, "[backup] init failed: %v (continuing)\n", err)
			return
		}
		if err := backup.AddRemote(ctx, dir, cfg.BackupRemoteURL); err != nil {
			fmt.Fprintf(os.Stderr, "[backup] add remote failed: %v (continuing)\n", err)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "[backup] syncing...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if cfg.BackupMachineID != "" {
		_ = os.Setenv("PRAIMATE_BACKUP_MACHINE_ID", cfg.BackupMachineID)
		defer os.Unsetenv("PRAIMATE_BACKUP_MACHINE_ID")
	}
	exportBackupState(ctx, dir)
	action, st, err := backup.Sync(ctx, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] sync error: %v (continuing)\n", err)
		return
	}
	switch action {
	case backup.SyncActionInSync:
		fmt.Fprintln(os.Stderr, "[backup] in sync ✓")
	case backup.SyncActionPushed:
		fmt.Fprintln(os.Stderr, "[backup] pushed local changes ✓")
		cfg.BackupLastSyncAt = time.Now().UTC()
		_ = launcher.SaveConfig(cfg)
	case backup.SyncActionPulled:
		fmt.Fprintln(os.Stderr, "[backup] pulled remote changes ✓")
		cfg.BackupLastSyncAt = time.Now().UTC()
		_ = launcher.SaveConfig(cfg)
	case backup.SyncActionNeedsResolution:
		if cfg.BackupForceAlwaysLocal {
			if guardOK := checkMachineGuard(ctx, cfg, dir); !guardOK {
				return
			}
			fmt.Fprintln(os.Stderr, "[backup] diverged; force-pushing local (force-always-local is ON)")
			if err := backup.Push(ctx, dir, true); err != nil {
				fmt.Fprintf(os.Stderr, "[backup] force push failed: %v (continuing)\n", err)
			} else {
				cfg.BackupLastSyncAt = time.Now().UTC()
				_ = launcher.SaveConfig(cfg)
			}
		} else {
			fmt.Fprintln(os.Stderr,
				"[backup] LOCAL AND REMOTE HAVE DIVERGED. Open the Backup tab "+
					"(Ctrl-4) to resolve. Status: "+strings.TrimSpace(fmt.Sprintf(
					"%d ahead, %d behind", st.Ahead, st.Behind)))
		}
	case backup.SyncActionNoRemote:
		// nothing to do
	}
}

// runExitAutoSync mirrors runStartupAutoSync for the exit path. Same
// rules; we just bias toward "commit and push" since exit time is
// almost always when the user expects their work to be saved.
// exportBackupState snapshots the PrAImate DB + agents into the backup
// repo before a sync, so the git-based backup captures the 1.1 state
// (chats, agents, memory, MCP) that lives in ~/.praimate, not just the
// on-disk chat sandboxes. Best-effort: a failure is logged and the sync
// proceeds (better to back up the sandboxes than nothing).
func exportBackupState(ctx context.Context, repoDir string) {
	c := getAppCore()
	if c == nil {
		return
	}
	if err := c.ExportBackupState(ctx, repoDir); err != nil {
		fmt.Fprintf(os.Stderr, "[backup] state export failed: %v (continuing)\n", err)
	}
}

func runExitAutoSync(cfg *launcher.Config) {
	dir := cfg.WorkspacesRoot
	if !backup.IsGitRepo(dir) {
		return
	}
	fmt.Fprintln(os.Stderr, "[backup] syncing on exit...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if cfg.BackupMachineID != "" {
		_ = os.Setenv("PRAIMATE_BACKUP_MACHINE_ID", cfg.BackupMachineID)
		defer os.Unsetenv("PRAIMATE_BACKUP_MACHINE_ID")
	}
	exportBackupState(ctx, dir)
	action, _, err := backup.Sync(ctx, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] sync error: %v\n", err)
		return
	}
	switch action {
	case backup.SyncActionInSync:
		fmt.Fprintln(os.Stderr, "[backup] in sync ✓")
	case backup.SyncActionPushed:
		fmt.Fprintln(os.Stderr, "[backup] pushed local changes ✓")
		cfg.BackupLastSyncAt = time.Now().UTC()
		_ = launcher.SaveConfig(cfg)
	case backup.SyncActionPulled:
		fmt.Fprintln(os.Stderr, "[backup] pulled remote changes ✓")
		cfg.BackupLastSyncAt = time.Now().UTC()
		_ = launcher.SaveConfig(cfg)
	case backup.SyncActionNeedsResolution:
		if cfg.BackupForceAlwaysLocal {
			if guardOK := checkMachineGuard(ctx, cfg, dir); !guardOK {
				return
			}
			fmt.Fprintln(os.Stderr, "[backup] diverged; force-pushing local (force-always-local is ON)")
			if err := backup.Push(ctx, dir, true); err != nil {
				fmt.Fprintf(os.Stderr, "[backup] force push failed: %v\n", err)
			} else {
				cfg.BackupLastSyncAt = time.Now().UTC()
				_ = launcher.SaveConfig(cfg)
			}
		} else {
			fmt.Fprintln(os.Stderr,
				"[backup] LOCAL AND REMOTE HAVE DIVERGED. Run Clade again "+
					"and open the Backup tab (Ctrl-4) to resolve.")
		}
	}
}

// checkMachineGuard refuses force-push when the remote's last commit
// has a Machine-ID trailer that doesn't match ours AND landed within
// the last 24h — i.e. another machine pushed recently and we'd be
// clobbering its work. The user has to disable force-always-local
// or resolve manually via Sync now.
func checkMachineGuard(ctx context.Context, cfg *launcher.Config, dir string) bool {
	remoteID, remoteWhen, err := backup.LastCommitMachineID(ctx, dir)
	if err != nil {
		// Best-effort: missing trailers / fetch failure → allow.
		return true
	}
	if remoteID == "" || remoteID == cfg.BackupMachineID {
		return true
	}
	if time.Since(remoteWhen) > 24*time.Hour {
		return true
	}
	fmt.Fprintf(os.Stderr,
		"[backup] Another machine (id %s) pushed to this remote within the\n"+
			"         last 24h (at %s). Refusing to force-push to avoid\n"+
			"         overwriting their work. Disable 'force always local' in\n"+
			"         the Backup tab or resolve manually via Sync now.\n",
		remoteID, remoteWhen.Format(time.RFC3339))
	return false
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

// runMergeMemory is wired into the workspaces repo via .git/config as
// the `clade-memory` merge driver. Git calls us with 3 args:
//
//	%O = ancestor (base) file
//	%A = our file (write the result here)
//	%B = their file
//
// We concatenate ours + theirs under a separator instead of producing
// `<<<<<<<` conflict markers. Best-effort: any error returns a
// non-zero exit, which makes git fall back to the default text merge.
func runMergeMemory(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "praimate --merge-memory needs 3 paths (ancestor, ours, theirs)")
		return 2
	}
	_ = args[0] // ancestor unused — we concatenate, not 3-way merge
	ours, theirs := args[1], args[2]
	a, errA := os.ReadFile(ours)
	if errA != nil {
		fmt.Fprintf(os.Stderr, "read ours: %v\n", errA)
		return 1
	}
	b, errB := os.ReadFile(theirs)
	if errB != nil {
		fmt.Fprintf(os.Stderr, "read theirs: %v\n", errB)
		return 1
	}
	combined := string(a)
	if !strings.HasSuffix(combined, "\n") {
		combined += "\n"
	}
	combined += "\n## --- merged from another machine at " +
		time.Now().UTC().Format(time.RFC3339) + " ---\n\n"
	combined += string(b)
	if !strings.HasSuffix(combined, "\n") {
		combined += "\n"
	}
	if err := os.WriteFile(ours, []byte(combined), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write ours: %v\n", err)
		return 1
	}
	return 0
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "praimate: %v\n", err)
	os.Exit(1)
}

// runCheckUpdate prints whether a newer release exists on GitHub. Used
// by the -check-update flag; safe to run in scripts (no side effects,
// no prompts).
func runCheckUpdate() int {
	rel, err := updater.FetchLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "praimate: %v\n", err)
		return 1
	}
	if updater.IsNewer(rel.TagName, version.Current) {
		fmt.Printf("update available: %s (currently %s)\n  %s\n", rel.TagName, version.Current, rel.HTMLURL)
		fmt.Println("\nRun `praimate -update` to install it.")
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
		fmt.Fprintf(os.Stderr, "praimate: %v\n", err)
		return 1
	}
	if !updater.IsNewer(rel.TagName, version.Current) {
		fmt.Printf("already on the latest release (%s)\n", version.Current)
		return 0
	}
	asset, err := updater.AssetForHost(rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "praimate: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "praimate: update failed: %v\n", err)
		return 1
	}
	fmt.Printf("\n✓ installed %s. Re-run `praimate` to start the new version.\n", rel.TagName)
	return 0
}

// runInstallTool installs a single Clade-managed tool (e.g. graphify)
// via the same installer.Run path the TUI Tools tab uses.
// Streams pnpm/uv progress to stdout so the user sees what's happening.
// Returns 0 on success, non-zero on every failure path.
//
// Anything else returns a helpful "available tools: ..." error so the
// user can pick.
func runInstallTool(name string) int {
	id := installer.ToolID(name)
	known := false
	var hint string
	for _, t := range installer.KnownTools() {
		hint += " " + string(t.ID)
		if t.ID == id {
			known = true
		}
	}
	if !known {
		fmt.Fprintf(os.Stderr, "praimate: unknown tool %q. Available:%s\n", name, hint)
		return 2
	}
	methods := installer.ToolMethods(id, installer.ActionInstall, installer.DetectOS())
	if len(methods) == 0 {
		fmt.Fprintf(os.Stderr, "praimate: no install method available for %s on this OS\n", name)
		fmt.Fprintf(os.Stderr, "       (common missing prereqs: uv for graphify/scrapegraph; git+bun+bash for gstack)\n")
		fmt.Fprintf(os.Stderr, "       uv: curl -LsSf https://astral.sh/uv/install.sh | sh   on Linux/macOS\n")
		fmt.Fprintf(os.Stderr, "       uv: irm https://astral.sh/uv/install.ps1 | iex        on Windows\n")
		return 1
	}
	m := methods[0]
	fmt.Printf("Installing %s\n  method: %s\n  command: %s\n\n", name, m.Label, m.Command)
	if err := installer.Run(context.Background(), m, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "\nclade: install %s failed: %v\n", name, err)
		return 1
	}
	fmt.Printf("\n✓ %s installed. New chats / sandboxes will pick it up automatically.\n", name)
	fmt.Printf("  (current Clade processes already have the bin dir on PATH via ImportClademToolsToPath)\n")
	return 0
}
