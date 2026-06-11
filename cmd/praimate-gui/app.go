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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/installer"
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
}

func NewApp() *App { return &App{terms: newTermManager()} }

// startup opens the shared DB and seeds builtins. Mirrors the TUI's
// initAppCore: failures are recorded, not fatal — the frontend shows
// a setup-error banner via Health().
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

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
	c, err := core.New(core.Options{Store: st})
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
	core.RegisterCLIAdapter(core.NewClaudeAdapter())

	// Same automation daemons the TUI runs — watchers and schedules
	// fire regardless of which surface is open.
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

	a.st = st
	a.core = c
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
func (a *App) StartTerminal(agentID, cli, cwd string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	if cwd == "" {
		return "", fmt.Errorf("a project folder is required")
	}
	name, args, err := terminalCommand(cli)
	if err != nil {
		return "", err
	}
	if agentID != "" {
		if agent, gerr := c.GetAgent(a.ctx, agentID); gerr == nil {
			_ = exportAgentContext(cwd, cli, agent)
		}
	}
	return a.terms.start(a.ctx, name, args, cwd, nil)
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

// InstallPraimateCode downloads the prebuilt PrAImate Code binary into
// the managed bin dir. Returns the install log on success so the
// frontend can show what happened. Synchronous — it's a single download.
func (a *App) InstallPraimateCode() (string, error) {
	var buf strings.Builder
	if err := installer.InstallPraimateCode(a.ctx, &buf); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
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
	return c.StartInteractiveChat(a.ctx, agentID, cli, cwd)
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
			systemPrompt = agent.Instructions
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

// --- Agents ----------------------------------------------------------------

func (a *App) ListAgents() ([]core.Agent, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListAgents(a.ctx)
}

// ImportAgentDialog opens a native file picker and imports the chosen
// YAML. Returns the imported agent, or nil if the user cancelled.
func (a *App) ImportAgentDialog() (*core.Agent, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Import agent YAML",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Agent YAML", Pattern: "*.yaml;*.yml"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	return c.ImportAgent(a.ctx, path)
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
