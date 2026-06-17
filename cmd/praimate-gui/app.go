package main

// App is the Wails binding surface. Every exported method becomes a
// JS-callable function under window.go.main.App.*. All methods are
// thin delegations into internal/core — the GUI holds NO business
// logic (plan §1 iron rule).
//
// Error convention: methods return (T, error); Wails surfaces the
// error as a rejected promise so the frontend can toast it.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sPROFFEs/PrAImate/internal/backup"
	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
	"github.com/sPROFFEs/PrAImate/internal/store"
	"github.com/sPROFFEs/PrAImate/internal/version"
)

// App carries the shared Core plus the Wails context used for dialogs
// and event emission.
type App struct {
	ctx     context.Context
	st      *store.Store
	core    *core.Core
	initErr string

	// daemonMu guards the daemon handles — Wails dispatches each
	// binding call on its own goroutine, so two watcher mutations can
	// race on restartWatchers without it.
	daemonMu  sync.Mutex
	watchers  *core.WatcherDaemon
	schedules *core.ScheduleDaemon

	terms *termManager

	// chatCancels maps chatID → cancel func for the in-flight streamed
	// turn, so the Stop button can interrupt it. Guarded by chatCancelMu
	// (binding calls run on independent goroutines).
	chatCancelMu sync.Mutex
	chatCancels  map[string]context.CancelFunc

	// approval is the lazily-started mid-turn approval broker ("ask"
	// Tools level). Guarded by approvalMu.
	approvalMu sync.Mutex
	approval   *approvalBroker

	// editorOwnWrites suppresses fsnotify echoes of the studio's own
	// file flushes (editor_window.go). Guarded by editorMu.
	editorMu        sync.Mutex
	editorOwnWrites map[string]time.Time
}

func NewApp() *App {
	return &App{terms: newTermManager(), chatCancels: map[string]context.CancelFunc{}}
}

// startup opens the shared DB and seeds builtins. Mirrors the TUI's
// initAppCore: failures are recorded, not fatal — the frontend shows
// a setup-error banner via Health().
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Same PATH augmentation the TUI does at startup: managed tool
	// prefixes (graphify, gstack, scrapegraph) and the praimate bin dir
	// (praimate-code). Without it the GUI — and every CLI child it
	// spawns — can't resolve tools installed into the managed dirs:
	// "graphify installs but isn't detected".
	installer.ImportManagedToolsToPath()
	installer.ImportPraimateBinToPath()

	// Live PATH rescan — periodically check the well-known per-user CLI
	// dirs in case the user installed bun/pnpm/cargo/uv in another
	// terminal while the GUI was already running. Cheap: just os.Stat
	// per dir, no fork. Goroutine ends when ctx is canceled (i.e. on
	// shutdown). The interval is wide enough not to be a busy loop and
	// tight enough that "I just ran `curl … | bash`" feels immediate.
	go func() {
		t := time.NewTicker(8 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				installer.ImportPnpmPathIfPresent()
				installer.ImportManagedToolsToPath()
				installer.ImportPraimateBinToPath()
				installer.ImportUserBinDirs()
			}
		}
	}()

	dbPath, err := store.DefaultDBPath()
	if err != nil {
		a.initErr = err.Error()
		return
	}
	st, err := store.Open(dbPath)
	if err != nil {
		a.initErr = err.Error()
		return
	}
	// Share the TUI's workspaces root (best-effort): with it, the GUI
	// can list the TUI's on-disk chats and reopen them in the Code
	// terminal. Without a config the GUI still works — the workspace
	// chats section just stays empty.
	workspacesRoot := ""
	if cfg, cfgErr := launcher.LoadConfig(); cfgErr == nil && cfg != nil {
		workspacesRoot = cfg.WorkspacesRoot
	}
	c, err := core.New(core.Options{Store: st, WorkspacesRoot: workspacesRoot})
	if err != nil {
		_ = st.Close()
		a.initErr = err.Error()
		return
	}
	if _, err := c.SeedBuiltins(ctx); err != nil {
		_ = st.Close()
		a.initErr = err.Error()
		return
	}
	core.RegisterAllCLIAdapters()
	// From here on, every backup commit snapshots the DB + shareable
	// config, and every pull/merge/reset row-merges the remote's
	// snapshot back in. Must precede any Backup-tab binding call.
	backup.SetStateSyncer(coreStateSyncer{core: c})
	// "Ask" Tools level: chats route mid-turn permission requests to
	// the GUI's Allow/Deny card (claude/openclaude only; see
	// approval_broker.go).
	c.SetApprovalProvider(a.approvalProvider)

	if editorFolder == "" {
		// Same automation daemons the TUI runs — watchers and schedules
		// fire regardless of which surface is open. NOT in studio-window
		// processes: the main window already runs them, and two daemons
		// on one DB would double-fire every schedule.
		watchers, _ := c.StartWatcherDaemon(context.Background(), core.WatcherDaemonOptions{
			WatcherDispatchOptions: core.WatcherDispatchOptions{CLI: "claude"},
		})
		schedules, _ := c.StartScheduleDaemon(context.Background(), core.ScheduleDaemonOptions{
			ScheduleDispatchOptions: core.ScheduleDispatchOptions{CLI: "claude"},
		})

		a.daemonMu.Lock()
		a.watchers = watchers
		a.schedules = schedules
		a.daemonMu.Unlock()
	}

	a.st = st
	a.core = c

	if editorFolder != "" {
		// Studio window: stream external file changes (the agent's
		// edits) into the open tabs.
		a.startEditorWatcher()
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.daemonMu.Lock()
	if a.watchers != nil {
		a.watchers.Stop()
		a.watchers = nil
	}
	if a.schedules != nil {
		a.schedules.Stop()
		a.schedules = nil
	}
	a.daemonMu.Unlock()
	if a.terms != nil {
		a.terms.closeAll()
	}
	if a.st != nil {
		_ = a.st.Close()
	}
}

// --- Terminal (live coding sessions) -------------------------------------

// PickProjectFolder opens a directory picker for a terminal's working
// folder.
func (a *App) PickProjectFolder() (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose project folder",
	})
}

// StartTerminal launches a third-party CLI live in a PTY for an agent.
// The agent's preferred CLI is used unless `cli` overrides it, run in
// `cwd`. The agent's instructions are exported into the folder's native
// context file (CLAUDE.md / AGENTS.md) so the CLI adopts the persona,
// without us reimplementing its loop. Returns the terminal session id;
// output streams over "term:data:<id>" events.
func (a *App) StartTerminal(agentID, cli, model, cwd, localEndpoint, localAPIKey, localModel string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	if cwd == "" {
		return "", fmt.Errorf("a project folder is required")
	}
	// A local endpoint routes by env; its model wins over the CLI model
	// box so claude/openclaude get --model pointed at the local model.
	var env []string
	if strings.TrimSpace(localEndpoint) != "" {
		if !terminalLocalRoutable(cli) {
			return "", fmt.Errorf("%s can't be routed to a local endpoint from a terminal — start a Chat with this CLI instead (it supports local LLMs fully)", cli)
		}
		if localModel != "" {
			model = localModel
		}
		env = terminalLocalEnv(cli, localEndpoint, localAPIKey, localModel)
	}
	name, args, err := terminalCommand(cli, model)
	if err != nil {
		return "", err
	}
	if agentID != "" {
		if agent, gerr := c.GetAgent(a.ctx, agentID); gerr == nil {
			_ = exportAgentContext(cwd, cli, agent)
		}
	}
	return a.terms.start(a.ctx, name, args, cwd, env)
}

// WriteTerminal forwards a base64-encoded chunk of keystrokes to the
// PTY. The frontend base64-encodes so arbitrary control bytes survive.
func (a *App) WriteTerminal(id, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode terminal input: %w", err)
	}
	return a.terms.write(id, data)
}

func (a *App) ResizeTerminal(id string, cols, rows int) error {
	return a.terms.resize(id, cols, rows)
}

func (a *App) CloseTerminal(id string) {
	a.terms.close(id)
}

// InstallPraimateCodeResult tells the frontend whether the download
// succeeded, failed for a fixable network reason, or failed because no
// prebuilt asset exists for this OS/arch (in which case the GUI offers
// "Compile from source" instead of letting the user retry forever).
type InstallPraimateCodeResult struct {
	OK              bool   `json:"ok"`
	Log             string `json:"log"`
	Error           string `json:"error,omitempty"`
	NoPrebuiltAsset bool   `json:"noPrebuiltAsset,omitempty"`
}

// InstallPraimateCode downloads the prebuilt PrAImate Code binary into
// the managed bin dir. Returns a structured result so the frontend can
// distinguish "404 / asset missing" (offer compile) from a generic
// failure (offer retry).
func (a *App) InstallPraimateCode() InstallPraimateCodeResult {
	var buf strings.Builder
	err := installer.InstallPraimateCode(a.ctx, &buf)
	res := InstallPraimateCodeResult{OK: err == nil, Log: buf.String()}
	if err != nil {
		res.Error = err.Error()
		if errors.Is(err, installer.ErrNoPrebuiltAsset) {
			res.NoPrebuiltAsset = true
		}
	}
	return res
}

// PraimateCodeInstalled reports whether praimate-code resolves on this
// host (managed bin dir or PATH).
func (a *App) PraimateCodeInstalled() bool {
	bin, err := installer.PraimateBinDir()
	if err == nil {
		name := "praimate-code"
		if osIsWindows() {
			name += ".exe"
		}
		if fi, e := os.Stat(filepath.Join(bin, name)); e == nil && !fi.IsDir() {
			return true
		}
	}
	_, err = exec.LookPath("praimate-code")
	return err == nil
}

func osIsWindows() bool { return runtime.GOOS == "windows" }

func (a *App) requireCore() (*core.Core, error) {
	if a.core == nil {
		if a.initErr != "" {
			return nil, fmt.Errorf("core unavailable: %s", a.initErr)
		}
		return nil, fmt.Errorf("core not initialised yet")
	}
	return a.core, nil
}

// --- Health / meta -------------------------------------------------------

// Health reports init status so the frontend can render a setup-error
// banner instead of failing every call individually.
type Health struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version"`
	DBPath  string `json:"db_path,omitempty"`
}

func (a *App) Health() Health {
	h := Health{Version: version.Current}
	if a.core == nil {
		h.Error = a.initErr
		return h
	}
	h.OK = true
	h.DBPath = a.st.Path()
	return h
}

// --- Chats ---------------------------------------------------------------

func (a *App) ListChats() ([]core.Chat, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListChats(a.ctx, 200)
}

// StartChat creates an interactive chat bound to an agent + CLI and
// returns it. The frontend then drives it with SendChat.
func (a *App) StartChat(agentID, cli, cwd string) (*core.Chat, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	chat, err := c.StartInteractiveChat(a.ctx, agentID, cli, cwd)
	if err != nil {
		return nil, err
	}
	a.applyDefaultSkills(chat.ID)
	return chat, nil
}

// SendChat sends one message into an interactive chat and returns the
// assistant's reply. The agent's instructions frame the first turn of a
// fresh session automatically.
func (a *App) SendChat(chatID, message string) (*core.ChatTurn, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	chat, err := c.GetChat(a.ctx, chatID)
	if err != nil {
		return nil, err
	}
	systemPrompt := ""
	if chat.AgentID != "" {
		if agent, err := c.GetAgent(a.ctx, chat.AgentID); err == nil {
			systemPrompt = core.AgentSystemPrompt(agent)
		}
	}
	if prefix := core.ResolveSkillsPrefix(chat.Settings.Skills); prefix != "" {
		if systemPrompt != "" {
			systemPrompt = prefix + "\n\n---\n\n" + systemPrompt
		} else {
			systemPrompt = prefix
		}
	}
	return c.ContinueChat(a.ctx, chatID, message, chat.WorkspacePath, systemPrompt)
}

func (a *App) ChatMessages(chatID string) ([]core.Message, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListMessages(a.ctx, chatID, 0)
}

func (a *App) DeleteChat(chatID string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteChat(a.ctx, chatID)
}

// --- Clean chats (CLI + model, no agent) ----------------------------------

// CLIInfo describes one launchable CLI for the "new chat" picker.
type CLIInfo struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Available bool     `json:"available"`
	ModelHint string   `json:"modelHint"` // expected --model format, "" = no model flag
	Models    []string `json:"models"`    // suggestions; free text always allowed
}

// modelHints documents each CLI's --model format. Empty string means
// the CLI has no model flag (the model input is disabled in the UI).
var modelHints = map[string]string{
	"claude":        "alias (sonnet, opus, haiku) or full model id",
	"openclaude":    "alias (sonnet, opus, haiku) or full model id",
	"codex":         "model id, e.g. gpt-5.1-codex",
	"opencode":      "provider/model, e.g. anthropic/claude-sonnet-4-5",
	"praimate-code": "provider/model, e.g. anthropic/claude-sonnet-4-5",
	"gemini":        "model id, e.g. gemini-2.5-pro",
	"deepseek":      "", // config-file driven; no per-run flag
}

// staticModelSuggestions are fallback datalist entries for CLIs without
// a list command. They are SUGGESTIONS — the input stays free text, so
// new models work without a PrAImate release.
var staticModelSuggestions = map[string][]string{
	"claude":     {"sonnet", "opus", "haiku", "claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"},
	"openclaude": {"sonnet", "opus", "haiku", "claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"},
	"codex":      {"gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1", "o4-mini"},
	"gemini":     {"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"},
}

// ListCLIs returns every launchable CLI with availability probed
// concurrently (a --version run each, bounded at 5s total).
func (a *App) ListCLIs() []CLIInfo {
	agents := launcher.KnownAgents()
	out := make([]CLIInfo, len(agents))
	// 5s was too tight: a slow `opencode --version` (Bun cold start)
	// or a stalled `codex --version` would race past the deadline and
	// the CLI got rendered as "not installed" in the chat/agent
	// selector even though the CLIs tab (30s budget) showed it
	// installed. 20s leaves headroom for the slowest healthy probe.
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, ag := range agents {
		out[i] = CLIInfo{
			ID:        string(ag.ID),
			Label:     ag.Label,
			ModelHint: modelHints[string(ag.ID)],
			Models:    staticModelSuggestions[string(ag.ID)],
		}
		adapter, err := core.GetCLIAdapter(string(ag.ID))
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(i int, ad core.CLIAdapter) {
			defer wg.Done()
			out[i].Available = ad.Available(ctx) == nil
		}(i, adapter)
	}
	wg.Wait()
	return out
}

// ListCLIModels returns live model suggestions for a CLI. For
// opencode/praimate-code it runs `<bin> models` (one provider/model per
// line); other CLIs fall back to the static suggestions.
func (a *App) ListCLIModels(cli string) []string {
	if cli != "opencode" && cli != "praimate-code" {
		return staticModelSuggestions[cli]
	}
	bin, err := exec.LookPath(cli)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "models")
	hideConsole(cmd)
	outBytes, err := cmd.Output()
	if err != nil {
		return nil
	}
	var models []string
	for _, line := range strings.Split(string(outBytes), "\n") {
		line = strings.TrimSpace(line)
		// Model lines are provider/model; skip log noise.
		if line != "" && strings.Contains(line, "/") && !strings.Contains(line, " ") {
			models = append(models, line)
		}
	}
	return models
}

// StartCleanChat creates an agent-less chat on a CLI with an optional
// pinned model. The frontend drives it with SendChat like any other
// chat; SendChat finds no AgentID and sends no system prompt.
func (a *App) StartCleanChat(cli, model, cwd string) (*core.Chat, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	chat, err := c.StartCleanChat(a.ctx, cli, model, cwd)
	if err != nil {
		return nil, err
	}
	a.applyDefaultSkills(chat.ID)
	return chat, nil
}

// --- Workspace (TUI) chats -------------------------------------------------

// WorkspaceChatInfo is one TUI on-disk chat, listed so the GUI can
// reopen it in the Code terminal. Read-only from the GUI side — the
// chat's workpath/sandbox stay owned by the TUI flow.
type WorkspaceChatInfo struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Agent    string    `json:"agent"`
	Template string    `json:"template"`
	LastUsed time.Time `json:"lastUsed"`
	Sandbox  string    `json:"sandbox"`
}

// ListWorkspaceChats lists the TUI's on-disk chats (newest first).
// Returns an empty list when no workspaces root is configured (TUI
// never ran on this machine).
func (a *App) ListWorkspaceChats() ([]WorkspaceChatInfo, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	chats, err := c.ListLegacyChats(a.ctx)
	if err != nil {
		return []WorkspaceChatInfo{}, nil // unconfigured root = empty, not an error
	}
	out := make([]WorkspaceChatInfo, 0, len(chats))
	for _, ch := range chats {
		out = append(out, WorkspaceChatInfo{
			ID: ch.ID, Label: ch.Label, Agent: string(ch.AgentID),
			Template: ch.Template, LastUsed: ch.LastUsed, Sandbox: ch.SandboxDir,
		})
	}
	return out, nil
}

// OpenWorkspaceChatResult is what OpenWorkspaceChat hands the frontend
// so the Code page can attach to the already-running terminal.
type OpenWorkspaceChatResult struct {
	TermID string `json:"termId"`
	CLI    string `json:"cli"`
	Cwd    string `json:"cwd"`
	Label  string `json:"label"`
	Note   string `json:"note"`
}

// OpenWorkspaceChat relaunches a TUI chat's CLI inside the GUI's Code
// terminal: same sandbox, same agent CLI, native session resume where
// the CLI supports it (claude/openclaude/codex). This is the
// TUI→GUI half of chat sharing; DB chats are the shared half both
// surfaces already read.
func (a *App) OpenWorkspaceChat(chatID string) (*OpenWorkspaceChatResult, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	root := c.WorkspacesRoot()
	if root == "" {
		return nil, fmt.Errorf("no workspaces root configured (run the TUI once first)")
	}
	chat, err := launcher.LoadChat(root, chatID)
	if err != nil || chat == nil {
		return nil, fmt.Errorf("load workspace chat %q: %w", chatID, err)
	}
	var agent *launcher.Agent
	for _, ag := range launcher.KnownAgents() {
		if ag.ID == chat.AgentID {
			ag := ag
			agent = &ag
			break
		}
	}
	if agent == nil {
		return nil, fmt.Errorf("unknown agent %q on chat %q", chat.AgentID, chatID)
	}
	name, args, err := terminalCommand(string(chat.AgentID), "")
	if err != nil {
		return nil, err
	}
	resume := launcher.RestoreNativeSession(*agent, *chat)
	args = append(args, resume.Args...)
	termID, err := a.terms.start(a.ctx, name, args, chat.SandboxDir, nil)
	if err != nil {
		return nil, err
	}
	a.terms.bindChat(termID, chatID)
	return &OpenWorkspaceChatResult{
		TermID: termID,
		CLI:    string(chat.AgentID),
		Cwd:    chat.SandboxDir,
		Label:  chat.Label,
		Note:   resume.Note,
	}, nil
}

// ListTerminalSessions returns the live PTYs the GUI is managing. The
// Sessions panel uses this to offer "resume" for terminal chats whose
// PTY is still alive (instead of starting a duplicate).
func (a *App) ListTerminalSessions() []TermInfo {
	if a.terms == nil {
		return nil
	}
	return a.terms.list()
}

// --- Agents ----------------------------------------------------------------

// hiddenAgentIDs are built-in agents that exist in the DB (so the studio
// helper / installer pipelines can use them) but should NOT show up in
// the GUI's Agents list — embedding-only agents the user shouldn't open.
var hiddenAgentIDs = map[string]bool{
	"agent-builder": true, // drives the New-Agent studio's authoring assistant
}

func (a *App) ListAgents() ([]core.Agent, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	all, err := c.ListAgents(a.ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, ag := range all {
		if hiddenAgentIDs[ag.ID] {
			continue
		}
		out = append(out, ag)
	}
	return out, nil
}

// ImportAgentDialog opens a native file picker and imports the chosen
// agent — bare YAML or a .praimate-agent knowledge pack. Returns the
// imported agent, or nil if the user cancelled.
func (a *App) ImportAgentDialog() (*core.Agent, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Import agent (YAML or pack)",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Agents", Pattern: "*.yaml;*.yml;*" + core.AgentPackExt + ";*.zip"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	return c.ImportAgentAuto(a.ctx, path)
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ExportAgentDialog opens a native save dialog and writes the agent
// YAML there. Returns the chosen path ("" if cancelled).
func (a *App) ExportAgentDialog(agentID string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Export agent YAML",
		DefaultFilename: agentID + ".yaml",
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := c.ExportAgent(a.ctx, agentID, path); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) DeleteAgent(agentID string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteAgent(a.ctx, agentID)
}

// --- Run (workflow execution) ---------------------------------------------

// PickFolder opens a directory picker for the run cwd.
func (a *App) PickFolder() (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose working folder",
	})
}

// TurnEvent is the payload emitted on the "praimate:turn" event while
// a workflow run is in flight.
type TurnEvent struct {
	Index      int    `json:"index"`
	UserMsg    string `json:"user_msg"`
	Reply      string `json:"reply"`
	DurationMs int64  `json:"duration_ms"`
}

// RunResult mirrors core.RunResult in a JSON-friendly shape.
type RunResult struct {
	Outcome   string      `json:"outcome"`
	Error     string      `json:"error,omitempty"`
	ChatID    string      `json:"chat_id,omitempty"`
	Turns     []TurnEvent `json:"turns"`
	SessionID string      `json:"session_id,omitempty"`
}

// RunWorkflow executes one agent workflow synchronously (the JS
// promise resolves when the run completes); per-turn progress streams
// via the "praimate:turn" event. Memory injection and persistence
// mirror the TUI Recipes flow.
func (a *App) RunWorkflow(agentID, workflowName, cli, cwd string, inputs map[string]string) (*RunResult, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	agent, err := c.GetAgent(a.ctx, agentID)
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = "."
	}

	query := ""
	for _, v := range inputs {
		query += v + " "
	}
	injection, _ := c.BuildMemoryInjection(a.ctx, core.InjectionOptions{Query: query})

	res := c.RunWorkflow(a.ctx, core.RunOptions{
		Agent:           agent,
		WorkflowName:    workflowName,
		Inputs:          inputs,
		CLI:             cli,
		Cwd:             cwd,
		Persist:         true,
		ChatTitle:       agent.Name + " · " + workflowName,
		MemoryInjection: injection,
		OnTurn: func(t core.TurnResult) {
			wruntime.EventsEmit(a.ctx, "praimate:turn", TurnEvent{
				Index:      t.Index,
				UserMsg:    t.UserMsg,
				Reply:      t.Reply.Text,
				DurationMs: t.DurationMs,
			})
		},
	})

	out := &RunResult{
		Outcome:   string(res.Outcome),
		ChatID:    res.ChatID,
		SessionID: res.SessionID,
	}
	if res.Err != nil {
		out.Error = res.Err.Error()
	}
	for _, t := range res.Turns {
		out.Turns = append(out.Turns, TurnEvent{
			Index: t.Index, UserMsg: t.UserMsg,
			Reply: t.Reply.Text, DurationMs: t.DurationMs,
		})
	}

	// Fire-and-forget distillation, same as the TUI Recipes flow.
	if res.ChatID != "" && res.Outcome == core.OutcomeCompleted {
		go func(chatID string) {
			_, _ = c.DistillChat(context.Background(), chatID, nil)
		}(res.ChatID)
	}
	return out, nil
}

// PrivacyPreview scans the would-be outbound text and returns category
// counts so the frontend can render a review sheet without ever
// echoing the secret values.
func (a *App) PrivacyPreview(text string) (map[string]int, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, m := range c.PrivacyScanner().Match(text) {
		counts[string(m.Category)]++
	}
	return counts, nil
}

// --- Memory ----------------------------------------------------------------

// MemorySnapshot bundles everything the Memory page renders in one call.
type MemorySnapshot struct {
	Enabled  bool              `json:"enabled"`
	Identity []core.Identity   `json:"identity"`
	Pinned   []core.PinnedFact `json:"pinned"`
	Episodes []core.Episode    `json:"episodes"`
}

func (a *App) GetMemory() (*MemorySnapshot, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	snap := &MemorySnapshot{}
	snap.Enabled, _ = c.IsMemoryEnabled(a.ctx)
	snap.Identity, _ = c.ListIdentity(a.ctx)
	snap.Pinned, _ = c.ListPinned(a.ctx, 0)
	snap.Episodes, _ = c.ListEpisodes(a.ctx, 100)
	return snap, nil
}

func (a *App) SetMemoryEnabled(enabled bool) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.SetMemoryEnabled(a.ctx, enabled)
}

func (a *App) SetIdentity(key, value string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.SetIdentity(a.ctx, key, value, "manual")
}

func (a *App) DeleteIdentity(key string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteIdentity(a.ctx, key)
}

func (a *App) PinFact(text string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	_, err = c.PinFact(a.ctx, text, 1.0)
	return err
}

func (a *App) DeletePinned(id int64) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeletePinned(a.ctx, id)
}

func (a *App) DeleteEpisode(id int64) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteEpisode(a.ctx, id)
}

// --- MCP ---------------------------------------------------------------------

func (a *App) MCPCatalogue() []core.MCPCatalogueEntry {
	entries := core.ListMCPCatalogue()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (a *App) MCPServers() ([]core.MCPServer, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListMCPServers(a.ctx, false)
}

// ConnectMCP connects a catalogue provider with an optional API key.
func (a *App) ConnectMCP(catalogueKey, apiKey string) (*core.MCPServer, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ConnectMCP(a.ctx, core.ConnectMCPRequest{
		CatalogueKey: catalogueKey,
		APIKey:       apiKey,
	})
}

// AddCustomMCP registers a user-defined MCP server (local or remote)
// that isn't in the catalogue — e.g. hexstrike-ai. envText is
// newline/comma-separated KEY=VALUE pairs.
func (a *App) AddCustomMCP(name, transport, command, url, envText string) (*core.MCPServer, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.AddCustomMCP(a.ctx, core.AddCustomMCPRequest{
		Name:      name,
		Transport: transport,
		Command:   command,
		URL:       url,
		Env:       core.ParseEnvLines(envText),
	})
}

func (a *App) SetMCPEnabled(id string, enabled bool) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.SetMCPEnabled(a.ctx, id, enabled)
}

func (a *App) DeleteMCPServer(id string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteMCPServer(a.ctx, id)
}

// --- Automation (watchers + schedules) --------------------------------------

func (a *App) ListWatchers() ([]core.Watcher, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListWatchers(a.ctx, false)
}

func (a *App) AddWatcher(agentID, path, workflow string, patterns []string) (int64, error) {
	c, err := a.requireCore()
	if err != nil {
		return 0, err
	}
	id, err := c.AddWatcher(a.ctx, core.AddWatcherRequest{
		AgentID: agentID, Path: path, Workflow: workflow, Patterns: patterns,
	})
	if err == nil {
		a.restartWatchers()
	}
	return id, err
}

func (a *App) SetWatcherEnabled(id int64, enabled bool) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	if err := c.SetWatcherEnabled(a.ctx, id, enabled); err != nil {
		return err
	}
	a.restartWatchers()
	return nil
}

func (a *App) DeleteWatcher(id int64) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	if err := c.DeleteWatcher(a.ctx, id); err != nil {
		return err
	}
	a.restartWatchers()
	return nil
}

// restartWatchers rebuilds the fsnotify daemon after watcher rows
// change so newly authored paths are observed immediately.
func (a *App) restartWatchers() {
	if a.core == nil {
		return
	}
	a.daemonMu.Lock()
	defer a.daemonMu.Unlock()
	if a.watchers != nil {
		a.watchers.Stop()
		a.watchers = nil
	}
	a.watchers, _ = a.core.StartWatcherDaemon(context.Background(), core.WatcherDaemonOptions{
		WatcherDispatchOptions: core.WatcherDispatchOptions{CLI: "claude"},
	})
}

func (a *App) ListSchedules() ([]core.Schedule, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListSchedules(a.ctx, false)
}

func (a *App) AddCronSchedule(agentID, cron, workflow string) (int64, error) {
	c, err := a.requireCore()
	if err != nil {
		return 0, err
	}
	return c.AddSchedule(a.ctx, core.AddScheduleRequest{
		AgentID: agentID, Cron: cron, Workflow: workflow,
	})
}

func (a *App) SetScheduleEnabled(id int64, enabled bool) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.SetScheduleEnabled(a.ctx, id, enabled)
}

func (a *App) DeleteSchedule(id int64) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeleteSchedule(a.ctx, id)
}

// --- Privacy patterns --------------------------------------------------------

func (a *App) ListPrivacyPatterns() ([]string, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListPrivacyPatterns(a.ctx)
}

func (a *App) AddPrivacyPattern(pattern string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.AddPrivacyPattern(a.ctx, pattern)
}

func (a *App) DeletePrivacyPattern(index int) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.DeletePrivacyPattern(a.ctx, index)
}

// --- GUI settings (ScopeGUI — never shared with the TUI, decision 8) --------

func (a *App) GetGUISetting(key string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	raw, err := c.GetSetting(a.ctx, core.ScopeGUI, key)
	if err != nil || raw == nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) SetGUISetting(key, valueJSON string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.SetSetting(a.ctx, core.ScopeGUI, key, []byte(valueJSON))
}
