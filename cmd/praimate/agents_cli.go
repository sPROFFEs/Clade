// CLI handlers for the new agent-launch flow added in Phase 2b. These
// flags are non-interactive utility paths exercised from the shell;
// the interactive TUI screens that wrap them ship in Phase 2c.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/store"
)

// openCore opens the default PrAImate DB, builds a Core, seeds the
// built-in agents, and registers production CLI adapters. Returns the
// Core and a cleanup function the caller must defer.
func openCore() (*core.Core, func(), error) {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve db path: %w", err)
	}
	st, err := store.Open(dbPath)
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
// facing utility, not a production UX). Use the GUI/TUI screens in
// Phase 2c for richer input collection.
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
