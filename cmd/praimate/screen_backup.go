package main

// Backup tab — manages the workspaces root as a git repo:
//   - configure remote URL (test, set, disconnect)
//   - manual sync (with the divergence-resolution popup)
//   - reset from remote (double-confirm)
//   - force push (double-confirm)
//   - auto-sync opt-in + the "force always local" sub-flag
//
// The screen is a list of actions: arrow-down to highlight, Enter to
// fire. Sub-modes (URL edit, divergence popup, confirmation prompts)
// take over the screen briefly and return.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/backup"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// Thin shims so the file can use lookup/set/unset env without
// re-importing os at every call site.
func lookupEnv(k string) (string, bool) { return os.LookupEnv(k) }
func setEnv(k, v string)                 { _ = os.Setenv(k, v) }
func unsetEnv(k string)                  { _ = os.Unsetenv(k) }

// backupRow indexes the action list. Keep in sync with renderActions.
type backupRow int

const (
	// The master toggle. Until this is on, every other row is
	// rendered grey + ignores Enter — flipping the switch is the
	// single explicit "yes, I want backup" gesture.
	backupRowEnabled backupRow = iota
	backupRowURL
	backupRowTest
	backupRowSync
	backupRowReset
	backupRowForcePush
	backupRowDisconnect
	backupRowAutoSync
	backupRowForceLocal
	backupRowCount
)

// backupMode tracks which sub-view is active. Most of the screen's
// life is spent in backupModeList; the others are short-lived modal
// overlays.
type backupMode int

const (
	backupModeList backupMode = iota
	backupModeEditURL
	backupModeDivergence
	backupModeConfirmReset1
	backupModeConfirmReset2
	backupModeConfirmForcePush
	backupModeBusy
	backupModeDone
)

type backupModel struct {
	cfg *launcher.Config

	cursor int
	mode   backupMode

	// last-known status snapshot, refreshed by Init/refresh action.
	status backup.Status

	// URL editor.
	urlInput textinput.Model

	// Divergence popup state.
	divLocal  []string // local commits not on remote
	divRemote []string // remote commits not local
	divCursor int      // 0..4 → m/r/p/R/esc

	// busy/done overlay text.
	busyMsg string
	doneMsg string
	doneErr error
}

func newBackupModel(cfg *launcher.Config) *backupModel {
	ti := textinput.New()
	ti.Placeholder = "https://github.com/<user>/<repo>.git"
	ti.CharLimit = 2048
	ti.Width = 60
	ti.SetValue(cfg.BackupRemoteURL)
	return &backupModel{
		cfg:      cfg,
		urlInput: ti,
	}
}

// --- Pane interface ---------------------------------------------------------

func (m *backupModel) Init() tea.Cmd { return m.refresh() }

func (m *backupModel) Title() string  { return "Backup · git sync" }
func (m *backupModel) NavSection() string { return navSectionBackup }
func (m *backupModel) CapturingInput() bool {
	return m.mode == backupModeEditURL
}
func (m *backupModel) Help() string {
	switch m.mode {
	case backupModeList:
		return "↑/↓ select · enter run · space toggle · esc back"
	case backupModeEditURL:
		return "enter save · esc cancel"
	case backupModeDivergence:
		return "↑/↓ select · enter run · esc cancel"
	case backupModeConfirmReset1, backupModeConfirmReset2:
		return "y to confirm · n / esc to cancel"
	case backupModeConfirmForcePush:
		return "y to confirm · n / esc to cancel"
	case backupModeBusy:
		return "(working...)"
	case backupModeDone:
		return "enter / esc to dismiss"
	}
	return ""
}

// --- messages ---------------------------------------------------------------

type backupStatusMsg struct {
	st backup.Status
}
type backupOpResultMsg struct {
	heading string
	err     error
}
type backupDivergenceMsg struct {
	st     backup.Status
	local  []string
	remote []string
}

// --- update -----------------------------------------------------------------

func (m *backupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case backupStatusMsg:
		m.status = msg.st
		return m, nil

	case backupDivergenceMsg:
		m.status = msg.st
		m.divLocal = msg.local
		m.divRemote = msg.remote
		m.divCursor = 0
		m.mode = backupModeDivergence
		return m, nil

	case backupOpResultMsg:
		m.mode = backupModeDone
		m.doneMsg = msg.heading
		m.doneErr = msg.err
		// Refresh status on the way out so the list reflects reality.
		return m, m.refresh()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *backupModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case backupModeList:
		return m.updateList(msg)
	case backupModeEditURL:
		return m.updateEditURL(msg)
	case backupModeDivergence:
		return m.updateDivergence(msg)
	case backupModeConfirmReset1, backupModeConfirmReset2, backupModeConfirmForcePush:
		return m.updateConfirm(msg)
	case backupModeDone:
		switch msg.String() {
		case "enter", "esc":
			m.mode = backupModeList
			m.doneMsg = ""
			m.doneErr = nil
		}
	}
	return m, nil
}

func (m *backupModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < int(backupRowCount)-1 {
			m.cursor++
		}
	case "r":
		// `r` refresh (the chat list also uses `r`).
		return m, m.refresh()
	case " ":
		// Inline toggles for the booleans.
		switch backupRow(m.cursor) {
		case backupRowEnabled:
			return m.toggleEnabled()
		case backupRowAutoSync:
			if !m.cfg.BackupEnabled {
				return m, nil
			}
			m.cfg.BackupAutoSync = !m.cfg.BackupAutoSync
			if m.cfg.BackupAutoSync && m.cfg.BackupMachineID == "" {
				m.cfg.BackupMachineID = newMachineID()
			}
			_ = launcher.SaveConfig(m.cfg)
		case backupRowForceLocal:
			if !m.cfg.BackupEnabled || !m.cfg.BackupAutoSync {
				return m, nil
			}
			m.cfg.BackupForceAlwaysLocal = !m.cfg.BackupForceAlwaysLocal
			_ = launcher.SaveConfig(m.cfg)
		}
	case "enter":
		return m.fireAction()
	}
	return m, nil
}

// toggleEnabled is the master switch. Flipping ON initialises the
// workspaces root as a git repo, writes the managed metadata,
// registers the MEMORY.md merge driver. Flipping OFF removes the
// configured remote, disables auto-sync, and leaves the .git dir
// alone so flipping back ON is cheap.
func (m *backupModel) toggleEnabled() (tea.Model, tea.Cmd) {
	if m.cfg.BackupEnabled {
		// Going from enabled → disabled.
		m.cfg.BackupEnabled = false
		m.cfg.BackupAutoSync = false
		m.cfg.BackupForceAlwaysLocal = false
		_ = launcher.SaveConfig(m.cfg)
		// Best-effort: drop the remote so a stale URL doesn't leak.
		if backup.IsGitRepo(m.cfg.WorkspacesRoot) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = backup.RemoveRemote(ctx, m.cfg.WorkspacesRoot)
		}
		return m, nil
	}
	// Going from disabled → enabled. Heavyweight: spawn the init
	// flow in a background Cmd so the user sees the busy spinner.
	m.mode = backupModeBusy
	m.busyMsg = "Enabling backup (initialising " + m.cfg.WorkspacesRoot + " as a git repo)..."
	dir := m.cfg.WorkspacesRoot
	cfg := m.cfg
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := backup.Init(ctx, dir); err != nil {
			return backupOpResultMsg{heading: "Enable backup failed (git init)", err: err}
		}
		cfg.BackupEnabled = true
		if cfg.BackupMachineID == "" {
			cfg.BackupMachineID = newMachineID()
		}
		if err := launcher.SaveConfig(cfg); err != nil {
			return backupOpResultMsg{heading: "Enable backup failed (save config)", err: err}
		}
		return backupOpResultMsg{heading: "Backup enabled ✓ — set a Remote URL next, then Sync now"}
	}
}

func (m *backupModel) fireAction() (tea.Model, tea.Cmd) {
	// The master toggle is always reachable; every other row is
	// gated on BackupEnabled. A disabled row is a no-op on Enter so
	// the user can't accidentally run a sync against an unconfigured
	// state.
	if backupRow(m.cursor) == backupRowEnabled {
		return m.toggleEnabled()
	}
	if !m.cfg.BackupEnabled {
		// Tell the user what's blocking them rather than just
		// silently doing nothing.
		m.mode = backupModeDone
		m.doneMsg = "Backup is disabled. Enable it from the first row to use this feature."
		return m, nil
	}
	switch backupRow(m.cursor) {
	case backupRowURL:
		m.urlInput.SetValue(m.cfg.BackupRemoteURL)
		m.urlInput.Focus()
		m.mode = backupModeEditURL
		return m, textinput.Blink

	case backupRowTest:
		return m, m.startTest()

	case backupRowSync:
		return m, m.startSync()

	case backupRowReset:
		m.mode = backupModeConfirmReset1
		return m, nil

	case backupRowForcePush:
		m.mode = backupModeConfirmForcePush
		return m, nil

	case backupRowDisconnect:
		return m, m.startDisconnect()

	case backupRowAutoSync:
		m.cfg.BackupAutoSync = !m.cfg.BackupAutoSync
		if m.cfg.BackupAutoSync && m.cfg.BackupMachineID == "" {
			m.cfg.BackupMachineID = newMachineID()
		}
		_ = launcher.SaveConfig(m.cfg)

	case backupRowForceLocal:
		if m.cfg.BackupAutoSync {
			m.cfg.BackupForceAlwaysLocal = !m.cfg.BackupForceAlwaysLocal
			_ = launcher.SaveConfig(m.cfg)
		}
	}
	return m, nil
}

func (m *backupModel) updateEditURL(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = backupModeList
		m.urlInput.Blur()
		return m, nil
	case tea.KeyEnter:
		url := strings.TrimSpace(m.urlInput.Value())
		m.cfg.BackupRemoteURL = url
		if err := launcher.SaveConfig(m.cfg); err != nil {
			m.doneMsg = "Save failed"
			m.doneErr = err
			m.mode = backupModeDone
			return m, nil
		}
		// If the workspaces root is already a git repo, update the
		// remote in place. If not, defer init+add to the next Sync.
		if url != "" && backup.IsGitRepo(m.cfg.WorkspacesRoot) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = backup.AddRemote(ctx, m.cfg.WorkspacesRoot, url)
		}
		m.urlInput.Blur()
		m.mode = backupModeList
		return m, m.refresh()
	}
	var cmd tea.Cmd
	m.urlInput, cmd = m.urlInput.Update(msg)
	return m, cmd
}

func (m *backupModel) updateDivergence(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = backupModeList
		return m, nil
	case "up", "k":
		if m.divCursor > 0 {
			m.divCursor--
		}
	case "down", "j":
		if m.divCursor < 3 {
			m.divCursor++
		}
	case "m":
		return m, m.runOp("merge", func(ctx context.Context) error {
			return backup.MergeFromRemote(ctx, m.cfg.WorkspacesRoot)
		})
	case "r":
		return m, m.runOp("rebase", func(ctx context.Context) error {
			return backup.RebaseOntoRemote(ctx, m.cfg.WorkspacesRoot)
		})
	case "p":
		return m, m.runOp("force push", func(ctx context.Context) error {
			return backup.Push(ctx, m.cfg.WorkspacesRoot, true)
		})
	case "R":
		return m, m.runOp("reset to remote", func(ctx context.Context) error {
			return backup.ResetHardToRemote(ctx, m.cfg.WorkspacesRoot)
		})
	case "enter":
		switch m.divCursor {
		case 0:
			return m, m.runOp("merge", func(ctx context.Context) error {
				return backup.MergeFromRemote(ctx, m.cfg.WorkspacesRoot)
			})
		case 1:
			return m, m.runOp("rebase", func(ctx context.Context) error {
				return backup.RebaseOntoRemote(ctx, m.cfg.WorkspacesRoot)
			})
		case 2:
			return m, m.runOp("force push", func(ctx context.Context) error {
				return backup.Push(ctx, m.cfg.WorkspacesRoot, true)
			})
		case 3:
			return m, m.runOp("reset to remote", func(ctx context.Context) error {
				return backup.ResetHardToRemote(ctx, m.cfg.WorkspacesRoot)
			})
		}
	}
	return m, nil
}

func (m *backupModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n", "N", "esc":
		m.mode = backupModeList
	case "y", "Y":
		switch m.mode {
		case backupModeConfirmReset1:
			m.mode = backupModeConfirmReset2
		case backupModeConfirmReset2:
			return m, m.runOp("reset to remote", func(ctx context.Context) error {
				return backup.ResetHardToRemote(ctx, m.cfg.WorkspacesRoot)
			})
		case backupModeConfirmForcePush:
			return m, m.runOp("force push", func(ctx context.Context) error {
				// Implicit commit of local changes first.
				if err := backup.CommitLocalChanges(ctx, m.cfg.WorkspacesRoot, ""); err != nil {
					return err
				}
				return backup.Push(ctx, m.cfg.WorkspacesRoot, true)
			})
		}
	}
	return m, nil
}

// --- ops --------------------------------------------------------------------

func (m *backupModel) refresh() tea.Cmd {
	dir := m.cfg.WorkspacesRoot
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		st, _ := backup.CurrentStatus(ctx, dir)
		return backupStatusMsg{st: st}
	}
}

func (m *backupModel) startTest() tea.Cmd {
	url := m.cfg.BackupRemoteURL
	if url == "" {
		m.doneMsg = "No remote URL configured"
		m.doneErr = nil
		m.mode = backupModeDone
		return nil
	}
	m.mode = backupModeBusy
	m.busyMsg = "Testing connection to " + url + " ..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		branch, err := backup.LsRemote(ctx, url)
		if err != nil {
			return backupOpResultMsg{heading: "Connection test failed", err: err}
		}
		return backupOpResultMsg{
			heading: "Connection OK (default branch: " + branch + ")",
		}
	}
}

func (m *backupModel) startSync() tea.Cmd {
	dir := m.cfg.WorkspacesRoot
	url := m.cfg.BackupRemoteURL
	machineID := m.cfg.BackupMachineID
	m.mode = backupModeBusy
	m.busyMsg = "Syncing..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		// Ensure the workspaces root is a git repo + has the configured remote.
		if !backup.IsGitRepo(dir) {
			if _, err := backup.Init(ctx, dir); err != nil {
				return backupOpResultMsg{heading: "Sync failed (git init)", err: err}
			}
		}
		if url != "" {
			if err := backup.AddRemote(ctx, dir, url); err != nil {
				return backupOpResultMsg{heading: "Sync failed (set remote)", err: err}
			}
		}
		// Set the machine-ID for commit trailers.
		if machineID != "" {
			t := setenvForCmd(ctx, "PRAIMATE_BACKUP_MACHINE_ID", machineID)
			defer t()
		}
		action, st, err := backup.Sync(ctx, dir)
		if err != nil {
			return backupOpResultMsg{heading: "Sync failed", err: err}
		}
		switch action {
		case backup.SyncActionInSync:
			return backupOpResultMsg{heading: "In sync ✓"}
		case backup.SyncActionPushed:
			return backupOpResultMsg{heading: "Pushed local changes ✓"}
		case backup.SyncActionPulled:
			return backupOpResultMsg{heading: "Pulled remote changes ✓"}
		case backup.SyncActionNoRemote:
			return backupOpResultMsg{heading: "No remote configured"}
		case backup.SyncActionNeedsResolution:
			local := listCommits(ctx, dir, "origin/"+st.DefaultBranch+"..HEAD")
			remote := listCommits(ctx, dir, "HEAD..origin/"+st.DefaultBranch)
			return backupDivergenceMsg{st: st, local: local, remote: remote}
		}
		return backupOpResultMsg{heading: "Sync done"}
	}
}

func (m *backupModel) startDisconnect() tea.Cmd {
	dir := m.cfg.WorkspacesRoot
	m.cfg.BackupRemoteURL = ""
	m.cfg.BackupAutoSync = false
	m.cfg.BackupForceAlwaysLocal = false
	_ = launcher.SaveConfig(m.cfg)
	m.mode = backupModeBusy
	m.busyMsg = "Disconnecting..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if backup.IsGitRepo(dir) {
			_ = backup.RemoveRemote(ctx, dir)
		}
		return backupOpResultMsg{heading: "Remote disconnected (local files untouched)"}
	}
}

// runOp wraps a backup operation in the busy → done state machine.
func (m *backupModel) runOp(label string, fn func(context.Context) error) tea.Cmd {
	machineID := m.cfg.BackupMachineID
	m.mode = backupModeBusy
	m.busyMsg = label + "..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if machineID != "" {
			t := setenvForCmd(ctx, "PRAIMATE_BACKUP_MACHINE_ID", machineID)
			defer t()
		}
		if err := fn(ctx); err != nil {
			return backupOpResultMsg{heading: label + " failed", err: err}
		}
		return backupOpResultMsg{heading: label + " ✓"}
	}
}

// listCommits returns short-format log entries (`<hash> <date> <subject>`)
// for the given revspec. Used by the divergence popup.
func listCommits(ctx context.Context, dir, spec string) []string {
	r := backup.Run(ctx, dir, "log", "--format=%h %ai %s", spec)
	if r.Failed() {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// setenvForCmd sets an env var and returns a restorer. Stdlib doesn't
// give us a per-cmd env directly in the bubbletea callback context;
// process-level setenv is what `git commit`'s child env will inherit.
func setenvForCmd(ctx context.Context, k, v string) func() {
	old, hadOld := lookupEnv(k)
	setEnv(k, v)
	return func() {
		if hadOld {
			setEnv(k, old)
		} else {
			unsetEnv(k)
		}
	}
}

// --- view -------------------------------------------------------------------

func (m *backupModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m *backupModel) Body() string {
	switch m.mode {
	case backupModeEditURL:
		return m.renderURLEditor()
	case backupModeDivergence:
		return m.renderDivergence()
	case backupModeConfirmReset1:
		return m.renderConfirm(
			"Reset local to match remote?",
			"This will DELETE every local change that hasn't been pushed.\n"+
				"Press y to continue to the FINAL confirmation, or n / esc to cancel.")
	case backupModeConfirmReset2:
		return m.renderConfirm(
			"Reset — final confirmation",
			"Local commits NOT on the remote will be lost. There is no\n"+
				"recovery from this. Press y to proceed, n / esc to cancel.")
	case backupModeConfirmForcePush:
		return m.renderConfirm(
			"Force-push local to remote?",
			"This will overwrite the remote with your local state. Any\n"+
				"commits on the remote that aren't local will be lost.\n"+
				"Press y to confirm, n / esc to cancel.")
	case backupModeBusy:
		return hintStyle.Render(m.busyMsg)
	case backupModeDone:
		var b strings.Builder
		if m.doneErr != nil {
			b.WriteString(errorStyle.Render("✗ " + m.doneMsg))
			b.WriteString("\n\n")
			b.WriteString(descStyle.Render("  " + m.doneErr.Error()))
		} else {
			b.WriteString(okStyle.Render("✓ " + m.doneMsg))
		}
		return b.String()
	}
	return m.renderList()
}

func (m *backupModel) renderList() string {
	var b strings.Builder
	// Header.
	b.WriteString(hintStyle.Render(
		"Optional cloud backup over git. When disabled, the workspaces root remains a "+
			"plain directory: no .git, no managed files, no traffic to any remote.") + "\n\n")

	statusLine := descStyle.Render("disabled (master switch is off)")
	if m.cfg.BackupEnabled {
		statusLine = "enabled"
		if m.status.Initialized {
			if m.status.HasRemote {
				statusLine += " · remote configured"
				if m.status.InSync() {
					statusLine += " · in sync"
				} else if m.status.Diverged() {
					statusLine = errorStyle.Render("enabled · DIVERGED (" +
						itoaInline(m.status.Ahead) + " ahead, " + itoaInline(m.status.Behind) + " behind)")
				} else if m.status.LocalAheadOnly() {
					statusLine += " · " + availableStyle.Render(itoaInline(m.status.Ahead)+" ahead")
				} else if m.status.RemoteAheadOnly() {
					statusLine += " · " + missingStyle.Render(itoaInline(m.status.Behind)+" behind")
				}
			} else {
				statusLine += " · no remote URL set"
			}
		} else {
			statusLine += " · git repo not initialised yet"
		}
	}
	b.WriteString(subtitleStyle.Render("Status:    ") + statusLine + "\n")
	if m.cfg.BackupLastSyncAt.IsZero() {
		b.WriteString(subtitleStyle.Render("Last sync: ") + descStyle.Render("never") + "\n")
	} else {
		b.WriteString(subtitleStyle.Render("Last sync: ") + m.cfg.BackupLastSyncAt.Format(time.RFC3339) + "\n")
	}
	if m.cfg.BackupMachineID != "" {
		b.WriteString(subtitleStyle.Render("Machine:   ") + descStyle.Render(m.cfg.BackupMachineID) + "\n")
	}
	b.WriteString("\n")

	// Rows.
	enabled := m.cfg.BackupEnabled
	gatedHint := descStyle.Render(" (enable backup first)")
	rows := []struct {
		label string
		value string
		hint  string
	}{
		{"Backup enabled (master switch)", boolValue(enabled),
			"Off by default. Turning it on initialises the workspaces root as a git " +
				"repo, writes the managed .gitignore + .gitattributes, and unlocks the " +
				"rest of this tab."},
		{"Remote URL", emptyOr(m.cfg.BackupRemoteURL, "(none)"),
			"Press Enter to edit. Public repos work without auth; private repos need " +
				"your git credentials configured."},
		{"Test connection", "[run git ls-remote]",
			"Probes the remote without modifying anything. Lightweight."},
		{"Sync now", "[run]",
			"Commits local changes and reconciles with the remote. Diverged repos open " +
				"a resolution popup (merge / rebase / force push / reset)."},
		{"Reset from remote", "[run · double-confirm]",
			"Discards every local change and matches the remote. Two confirmations to " +
				"avoid misclicks."},
		{"Force push", "[run · confirm]",
			"Overwrites the remote with the local state. Confirms once."},
		{"Disconnect", "[run]",
			"Clears the remote URL + disables auto-sync. Local files untouched."},
		{"Auto-sync", boolValue(m.cfg.BackupAutoSync),
			"Sync on every Clade startup and exit. Diverged repos open the resolution " +
				"popup unless 'force always local' is also on."},
		{"Force always local", forceLocalLabel(m),
			"⚠ Disables the divergence popup in auto-sync — local always wins. Two " +
				"machines both using this take turns silently overwriting each other's work."},
	}
	if !enabled {
		// Suffix the hint on every gated row so the user sees "you
		// need to enable backup first" without having to cursor onto
		// each one to discover it.
		for i := 1; i < len(rows); i++ {
			rows[i].hint += gatedHint
		}
	}
	for i, r := range rows {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		left := marker + r.label
		pad := 22 - lipglossDimWidth(left)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(selectionRow(left+strings.Repeat(" ", pad)+r.value, isSel) + "\n")
		if isSel {
			b.WriteString(descStyle.Render("   "+r.hint) + "\n")
		}
	}
	return b.String()
}

func (m *backupModel) renderURLEditor() string {
	var b strings.Builder
	b.WriteString(inputLabelStyle.Render("Remote URL: "))
	b.WriteString(m.urlInput.View() + "\n\n")
	b.WriteString(descStyle.Render(
		"Examples: https://github.com/<user>/<repo>.git · git@github.com:<user>/<repo>.git\n"+
			"Empty + Enter = clear the URL (effectively the same as Disconnect)."))
	return b.String()
}

func (m *backupModel) renderConfirm(title, body string) string {
	var b strings.Builder
	b.WriteString(errorStyle.Render("⚠ "+title) + "\n\n")
	b.WriteString(body + "\n")
	return b.String()
}

func (m *backupModel) renderDivergence() string {
	var b strings.Builder
	b.WriteString(errorStyle.Render("Local vs remote: DIVERGED") + "\n\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("Local has %d commits not on remote:", len(m.divLocal))) + "\n")
	if len(m.divLocal) == 0 {
		b.WriteString(descStyle.Render("  (none)") + "\n")
	} else {
		for _, c := range m.divLocal {
			b.WriteString(descStyle.Render("  "+c) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("Remote has %d commits not local:", len(m.divRemote))) + "\n")
	if len(m.divRemote) == 0 {
		b.WriteString(descStyle.Render("  (none)") + "\n")
	} else {
		for _, c := range m.divRemote {
			b.WriteString(descStyle.Render("  "+c) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("How to reconcile?") + "\n\n")
	opts := []struct {
		key, label, hint string
	}{
		{"m", "Merge", "Keep both sides; creates a merge commit. Standard git merge — falls back to manual resolution on conflicts."},
		{"r", "Rebase", "Replay local commits on top of remote. Cleaner history; same conflict rules as merge."},
		{"p", "Force push", "Discard remote; keep local. Destructive on the remote side."},
		{"R", "Reset", "Discard local; keep remote. Destructive on the local side."},
	}
	for i, opt := range opts {
		isSel := i == m.divCursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		b.WriteString(selectionRow(marker+"["+opt.key+"] "+opt.label, isSel) + "\n")
		if isSel {
			b.WriteString(descStyle.Render("    "+opt.hint) + "\n")
		}
	}
	b.WriteString("\n" + descStyle.Render("Or press esc to leave the divergence as-is for later."))
	return b.String()
}

// --- small helpers ----------------------------------------------------------

func newMachineID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func emptyOr(s, fallback string) string {
	if s == "" {
		return descStyle.Render(fallback)
	}
	return s
}

func itoaInline(n int) string { return fmt.Sprintf("%d", n) }

func forceLocalLabel(m *backupModel) string {
	if !m.cfg.BackupAutoSync {
		return descStyle.Render("(disabled — enable auto-sync first)")
	}
	if m.cfg.BackupForceAlwaysLocal {
		return errorStyle.Render("[x] on (force-push every sync)")
	}
	return descStyle.Render("[ ] off")
}

func lipglossDimWidth(s string) int {
	return renderListLabel(s)
}
