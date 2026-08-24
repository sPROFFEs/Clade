// Non-interactive agent utility handlers used by the maintenance CLI.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/ollama"
	"git.jtsec.local/lab/PrAImate/internal/store"
	"golang.org/x/term"
)

// openCore opens the default PrAImate DB, builds a Core, seeds the
// built-in agents, and registers production CLI adapters. Returns the
// Core and a cleanup function the caller must defer.
func openCore() (*core.Core, func(), error) {
	return openCoreWithPassword("")
}

// openCoreWithPassword opens secure storage with either the explicit
// password supplied by a headless caller or the remembered OS credential.
// Passwords are deliberately never accepted as command-line values because
// argv is visible to other local processes on common Linux configurations.
func openCoreWithPassword(password string) (*core.Core, func(), error) {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve db path: %w", err)
	}
	var st *store.Store
	if password != "" {
		st, err = store.OpenWithPassword(dbPath, password)
	} else {
		st, err = store.Open(dbPath)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	c, err := core.New(core.Options{Store: st})
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	if _, err := c.SeedBuiltins(context.Background()); err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("seed builtins: %w", err)
	}
	core.RegisterAllCLIAdapters()
	return c, func() { _ = st.Close() }, nil
}

const agentRunSchema = "praimate.agent-run/v1"

var (
	agentRunInput                io.Reader = os.Stdin
	agentRunOutput               io.Writer = os.Stdout
	agentRunError                io.Writer = os.Stderr
	openAgentRunCore                       = openCoreWithPassword
	readAgentRunTerminalPassword           = readTerminalPassword
	agentRunTerminalAvailable              = agentRunInputIsTerminal
)

type agentPromptOptions struct {
	AgentID         string
	CLI             string
	Folder          string
	Prompt          string
	PromptFile      string
	Model           string
	Endpoint        string
	Tools           string
	Output          string
	Timeout         time.Duration
	Persist         bool
	DBPasswordStdin bool
}

type agentRunResult struct {
	Schema     string `json:"schema"`
	OK         bool   `json:"ok"`
	AgentID    string `json:"agentId"`
	AgentName  string `json:"agentName,omitempty"`
	CLI        string `json:"cli,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	ChatID     string `json:"chatId,omitempty"`
	Reply      string `json:"reply"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

type agentRunEvent struct {
	Schema    string `json:"schema"`
	Type      string `json:"type"`
	AgentID   string `json:"agentId"`
	EventType string `json:"eventType,omitempty"`
	Text      string `json:"text,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Detail    string `json:"detail,omitempty"`
	OK        bool   `json:"ok,omitempty"`
}

func runAgentPrompt(opts agentPromptOptions) int {
	promptUsesStdin := opts.PromptFile == "-" || (opts.Prompt == "" && opts.PromptFile == "")
	if opts.DBPasswordStdin && promptUsesStdin {
		return writeAgentFailure(opts, 2, "stdin cannot carry both the prompt and database password; use --prompt or --prompt-file")
	}
	prompt, err := readAgentPrompt(opts)
	if err != nil {
		return writeAgentFailure(opts, 2, err.Error())
	}
	password := strings.TrimSpace(os.Getenv("PRAIMATE_DB_PASSWORD"))
	_ = os.Unsetenv("PRAIMATE_DB_PASSWORD")
	if opts.DBPasswordStdin {
		if agentRunTerminalAvailable() {
			password, err = readAgentRunTerminalPassword()
		} else {
			password, err = readPasswordLine(agentRunInput)
		}
		if err != nil {
			return writeAgentFailure(opts, 2, err.Error())
		}
	}

	folder, err := resolveAgentFolder(opts.Folder)
	if err != nil {
		return writeAgentFailure(opts, 2, err.Error())
	}
	tools := strings.ToLower(strings.TrimSpace(opts.Tools))
	endpoint, err := resolveAgentEndpoint(opts.Endpoint)
	if err != nil {
		return writeAgentFailure(opts, 2, err.Error())
	}
	model := strings.TrimSpace(opts.Model)
	if endpoint != "" && model == "" {
		return writeAgentFailure(opts, 2, "--endpoint requires --model to select the model served by that endpoint")
	}
	if tools == "safe" {
		tools = ""
	}
	if tools != "" && tools != "edits" && tools != "full" {
		return writeAgentFailure(opts, 2, "--tools must be safe, edits, or full")
	}
	if opts.Output == "" {
		opts.Output = "json"
	}
	if opts.Output != "json" && opts.Output != "jsonl" && opts.Output != "text" {
		return writeAgentFailure(opts, 2, "--output must be json, jsonl, or text")
	}

	c, cleanup, err := openAgentRunCore(password)
	if err != nil && password == "" && errors.Is(err, store.ErrPasswordRequired) && agentRunTerminalAvailable() {
		password, err = readAgentRunTerminalPassword()
		if err == nil {
			c, cleanup, err = openAgentRunCore(password)
		}
	}
	password = ""
	if err != nil {
		if errors.Is(err, store.ErrPasswordRequired) {
			err = errors.New("database password required: enable Remember password, set PRAIMATE_DB_PASSWORD, or use --db-password-stdin")
		}
		return writeAgentFailure(opts, 1, err.Error())
	}
	defer cleanup()

	ctx := context.Background()
	if opts.Timeout < 0 {
		return writeAgentFailure(opts, 2, "--timeout cannot be negative")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	agent, err := c.GetAgent(ctx, strings.TrimSpace(opts.AgentID))
	if err != nil {
		return writeAgentFailure(opts, 1, err.Error())
	}
	cli := strings.TrimSpace(opts.CLI)
	if cli == "" {
		if len(agent.Supports) == 0 {
			return writeAgentFailure(opts, 1, fmt.Sprintf("agent %q supports no CLI", agent.ID))
		}
		cli = agent.Supports[0]
	}

	// Managed tools fail closed unless the caller explicitly selected an
	// automation policy. "edits" approves only project writes; "full"
	// approves every capability declared by the agent runtime manifest.
	if tools != "" {
		c.SetApprovalProvider(func(string) *core.ApprovalConfig {
			return &core.ApprovalConfig{Request: func(_ context.Context, tool string, _ map[string]any) (bool, error) {
				return tools == "full" || (tools == "edits" && tool == "project.write"), nil
			}}
		})
	}

	chat, err := c.StartInteractiveChat(ctx, agent.ID, cli, folder)
	if err != nil {
		return writeAgentFailure(opts, 1, err.Error())
	}
	if !opts.Persist {
		defer func() { _ = c.DeleteChat(context.Background(), chat.ID) }()
	}
	// Always persist the policy, including explicit Safe (empty). Otherwise an
	// agent runtime's default_tools could silently widen a headless run.
	if err := c.UpdateChatConfig(ctx, chat.ID, cli, model, tools); err != nil {
		return writeAgentFailure(opts, 1, err.Error())
	}
	if endpoint != "" {
		if err := c.UpdateChatSettings(ctx, chat.ID, func(s *core.ChatSettings) {
			s.Model = ""
			s.Local = &core.ChatLocalEndpoint{Endpoint: endpoint, Model: model}
		}); err != nil {
			return writeAgentFailure(opts, 1, err.Error())
		}
	}
	effective, err := c.ResolveEffectiveAgentConfig(ctx, agent)
	if err != nil {
		return writeAgentFailure(opts, 1, err.Error())
	}

	started := time.Now()
	var onEvent core.StreamHandler
	if opts.Output == "jsonl" {
		enc := json.NewEncoder(agentRunOutput)
		onEvent = func(ev core.StreamEvent) {
			_ = enc.Encode(agentRunEvent{
				Schema: agentRunSchema, Type: "event", AgentID: agent.ID,
				EventType: ev.Type, Text: ev.Text, Tool: ev.Tool,
				Detail: ev.Detail, OK: ev.OK,
			})
		}
	}
	turn, err := c.ContinueChatStream(ctx, chat.ID, prompt, folder, core.AgentSystemPrompt(agent), nil, onEvent)
	duration := time.Since(started).Milliseconds()
	if err != nil {
		code := 1
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = 124
		}
		return writeAgentFailureWithMeta(opts, code, agent, cli, string(effective.Mode), chat.ID, duration, err.Error())
	}
	result := agentRunResult{
		Schema: agentRunSchema, OK: true, AgentID: agent.ID, AgentName: agent.Name,
		CLI: cli, Runtime: string(effective.Mode), Reply: turn.Reply,
		DurationMs: duration,
	}
	if opts.Persist {
		result.ChatID = chat.ID
	}
	return writeAgentResult(opts.Output, result)
}

func readAgentPrompt(opts agentPromptOptions) (string, error) {
	if opts.Prompt != "" && opts.PromptFile != "" {
		return "", errors.New("use only one of --prompt or --prompt-file")
	}
	var raw []byte
	var err error
	switch {
	case opts.PromptFile == "-":
		raw, err = io.ReadAll(agentRunInput)
	case opts.PromptFile != "":
		raw, err = os.ReadFile(opts.PromptFile)
	case opts.Prompt != "":
		raw = []byte(opts.Prompt)
	default:
		file, ok := agentRunInput.(*os.File)
		if ok {
			info, statErr := file.Stat()
			if statErr != nil || info.Mode()&os.ModeCharDevice != 0 {
				return "", errors.New("a prompt is required via --prompt, --prompt-file, or piped stdin")
			}
		}
		raw, err = io.ReadAll(agentRunInput)
	}
	if err != nil {
		return "", fmt.Errorf("read prompt: %w", err)
	}
	prompt := strings.TrimSpace(string(raw))
	if prompt == "" {
		return "", errors.New("prompt is empty")
	}
	return prompt, nil
}

func readPasswordLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 64<<10)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read database password: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("database password is empty")
	}
	return line, nil
}

func agentRunInputIsTerminal() bool {
	f, ok := agentRunInput.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// readTerminalPassword reads directly from the controlling stdin terminal
// with echo disabled. The prompt goes to stderr so stdout stays valid JSON.
func readTerminalPassword() (string, error) {
	f, ok := agentRunInput.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", store.ErrPasswordRequired
	}
	fmt.Fprint(agentRunError, "PrAImate database password: ")
	raw, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(agentRunError)
	if err != nil {
		return "", fmt.Errorf("read database password: %w", err)
	}
	password := strings.TrimSpace(string(raw))
	for i := range raw {
		raw[i] = 0
	}
	if password == "" {
		return "", errors.New("database password is empty")
	}
	return password, nil
}

func resolveAgentFolder(folder string) (string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", errors.New("--folder is required")
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", fmt.Errorf("resolve --folder: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("--folder is not an accessible directory: %s", abs)
	}
	return abs, nil
}

// resolveAgentEndpoint only permits the endpoint already selected in the GUI.
// Otherwise a headless caller could redirect the encrypted API key to an
// attacker-controlled URL. "saved" keeps automation portable across machines.
func resolveAgentEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	cfg, err := launcher.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("load saved local endpoint: %w", err)
	}
	if cfg == nil || strings.TrimSpace(cfg.DefaultLocalEndpoint) == "" {
		return "", errors.New("--endpoint requires a saved endpoint; configure one in the Local LLM tab first")
	}
	saved := strings.TrimSpace(cfg.DefaultLocalEndpoint)
	if strings.EqualFold(value, "saved") {
		return saved, nil
	}
	if ollama.NormalizeEndpoint(value) != ollama.NormalizeEndpoint(saved) {
		return "", errors.New("--endpoint must be 'saved' or match the endpoint configured in the Local LLM tab")
	}
	return saved, nil
}

func writeAgentFailure(opts agentPromptOptions, code int, message string) int {
	return writeAgentFailureWithMeta(opts, code, nil, opts.CLI, "", "", 0, message)
}

func writeAgentFailureWithMeta(opts agentPromptOptions, code int, agent *core.Agent, cli, runtime, chatID string, duration int64, message string) int {
	result := agentRunResult{Schema: agentRunSchema, AgentID: opts.AgentID, CLI: cli, Runtime: runtime, DurationMs: duration, Error: message}
	if agent != nil {
		result.AgentID, result.AgentName = agent.ID, agent.Name
	}
	if opts.Persist {
		result.ChatID = chatID
	}
	output := opts.Output
	if output == "" {
		output = "json"
	}
	if output == "text" {
		fmt.Fprintln(agentRunError, "praimate:", message)
		return code
	}
	_ = writeAgentResult(output, result)
	return code
}

func writeAgentResult(output string, result agentRunResult) int {
	if output == "text" {
		fmt.Fprintln(agentRunOutput, result.Reply)
		return 0
	}
	if output == "jsonl" {
		line := struct {
			Type string `json:"type"`
			agentRunResult
		}{Type: "result", agentRunResult: result}
		_ = json.NewEncoder(agentRunOutput).Encode(line)
		return 0
	}
	_ = json.NewEncoder(agentRunOutput).Encode(result)
	return 0
}

// runListAgents implements `praimate -list-agents`.
func runListAgents() int {
	c, cleanup, err := openCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate:", err)
		return 1
	}
	defer cleanup()

	agents, err := c.ListAgents(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate:", err)
		return 1
	}
	if len(agents) == 0 {
		fmt.Println("(no agents — built-ins should auto-seed; this is a bug)")
		return 0
	}
	fmt.Printf("%-22s %-12s %s\n", "ID", "SUPPORTS", "DESCRIPTION")
	for _, a := range agents {
		supports := strings.Join(a.Supports, ",")
		desc := strings.SplitN(a.Description, "\n", 2)[0]
		fmt.Printf("%-22s %-12s %s\n", a.ID, supports, desc)
	}
	return 0
}

// runAgentWorkflow implements `praimate -run-agent <id> [-cli ...] [-workflow ...] [-inputs k=v,...]`.
//
// Inputs use the same comma-separated key=value format as go test -run;
// quoted values with commas are not supported (this is a developer-
// facing utility, not the primary UX). Use the GUI for richer input
// collection.
func runAgentWorkflow(agentID, cli, workflow, inputsRaw string) int {
	c, cleanup, err := openCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate:", err)
		return 1
	}
	defer cleanup()

	ctx := context.Background()
	agent, err := c.GetAgent(ctx, agentID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate:", err)
		return 1
	}

	inputs := parseInputsCSV(inputsRaw)
	cwd, _ := os.Getwd()

	start := time.Now()
	res := c.RunWorkflow(ctx, core.RunOptions{
		Agent:        agent,
		WorkflowName: workflow,
		Inputs:       inputs,
		CLI:          cli,
		Cwd:          cwd,
		OnTurn: func(t core.TurnResult) {
			fmt.Printf("--- turn %d (%dms) ---\n", t.Index+1, t.DurationMs)
			fmt.Println(t.Reply.Text)
			fmt.Println()
		},
	})
	elapsed := time.Since(start)

	if res.Err != nil {
		fmt.Fprintf(os.Stderr, "praimate: workflow %s/%s failed (%s after %s): %v\n",
			res.AgentID, res.WorkflowName, res.Outcome, elapsed, res.Err)
		return 1
	}
	fmt.Printf("=== %s (%d turns in %s) ===\n", res.Outcome, len(res.Turns), elapsed)
	return 0
}

// parseInputsCSV parses "k=v,k2=v2" into a map. Empty input → empty map.
// Whitespace around keys/values is trimmed. Duplicate keys: last wins.
// Pairs without "=" are skipped silently — the runner's own validation
// will reject missing required inputs with a useful error.
func parseInputsCSV(s string) map[string]string {
	out := map[string]string{}
	if s == "" {
		return out
	}
	for _, pair := range strings.Split(s, ",") {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(pair[:eq])
		v := strings.TrimSpace(pair[eq+1:])
		out[k] = v
	}
	return out
}

// runImportTemplate implements `praimate -import-template <dir>`. It
// converts a pre-1.1 workpath template into an agent with its knowledge
// base. Pass a single dir, or a parent dir to import every template
// subdirectory inside it (skips _common and any dir without a template
// marker).
func runImportTemplate(path string) int {
	c, cleanup, err := openCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate:", err)
		return 1
	}
	defer cleanup()
	ctx := context.Background()

	var dirs []string
	if core.IsWorkpathTemplate(path) {
		dirs = []string{path}
	} else {
		entries, rerr := os.ReadDir(path)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "praimate:", rerr)
			return 1
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == "_common" || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			sub := filepath.Join(path, e.Name())
			if core.IsWorkpathTemplate(sub) {
				dirs = append(dirs, sub)
			}
		}
	}
	if len(dirs) == 0 {
		fmt.Fprintf(os.Stderr, "praimate: no workpath templates found under %s\n", path)
		return 1
	}

	rc := 0
	for _, d := range dirs {
		agent, err := c.ImportWorkpathTemplate(ctx, d, "", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", filepath.Base(d), err)
			rc = 1
			continue
		}
		files, _ := core.ListAgentKnowledge(agent.ID)
		fmt.Printf("  ✓ %s → agent %q (%d knowledge files)\n", filepath.Base(d), agent.ID, len(files))
	}
	return rc
}
