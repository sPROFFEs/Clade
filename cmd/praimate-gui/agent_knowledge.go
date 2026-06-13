package main

// Agent knowledge bindings — the GUI side of knowledge packs: pick
// docs (files or a whole folder) into the agent's knowledge dir, set
// the mode (raw folder vs graphify RAG), build/refresh the RAG index
// with streamed output, and import/export .praimate-agent packs.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// AgentKnowledgeInfo is the knowledge panel's state for one agent.
type AgentKnowledgeInfo struct {
	Mode              string   `json:"mode"` // "", "raw", "rag"
	Dir               string   `json:"dir"`
	Files             []string `json:"files"`
	GraphifyInstalled bool     `json:"graphifyInstalled"`
	HasIndex          bool     `json:"hasIndex"`
}

// GetAgentKnowledge reports the agent's knowledge state.
func (a *App) GetAgentKnowledge(id string) (*AgentKnowledgeInfo, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	agent, err := c.GetAgent(a.ctx, id)
	if err != nil {
		return nil, err
	}
	dir, err := core.AgentKnowledgeDir(id)
	if err != nil {
		return nil, err
	}
	files, err := core.ListAgentKnowledge(id)
	if err != nil {
		return nil, err
	}
	_, gerr := exec.LookPath("graphify")
	return &AgentKnowledgeInfo{
		Mode:              agent.Knowledge,
		Dir:               dir,
		Files:             files,
		GraphifyInstalled: gerr == nil,
		HasIndex:          dirExists(dir + "/graphify-out"),
	}, nil
}

// SetAgentKnowledgeMode persists the mode ("", "raw", "rag"). The
// documents stay where they are — only the launch guidance changes.
func (a *App) SetAgentKnowledgeMode(id, mode string) error {
	switch mode {
	case "", "raw", "rag":
	default:
		return fmt.Errorf("unknown knowledge mode %q", mode)
	}
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	agent, err := c.GetAgent(a.ctx, id)
	if err != nil {
		return err
	}
	agent.Knowledge = mode
	raw, err := core.MarshalAgentYAML(agent)
	if err != nil {
		return err
	}
	_, err = c.ImportAgentYAML(a.ctx, raw, agent.SourcePath)
	return err
}

// PickAgentKnowledgeFiles opens a multi-file dialog and copies the
// selection into the agent's knowledge folder. Returns the new list.
func (a *App) PickAgentKnowledgeFiles(id string) ([]string, error) {
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Add documents to the agent's knowledge base",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Documents", Pattern: "*.md;*.txt;*.pdf;*.csv;*.json;*.yaml;*.html;*.docx"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(paths) > 0 {
		if _, err := core.AddAgentKnowledgeFiles(id, paths); err != nil {
			return nil, err
		}
	}
	return core.ListAgentKnowledge(id)
}

// PickAgentKnowledgeFolder copies a whole folder into the knowledge base.
func (a *App) PickAgentKnowledgeFolder(id string) ([]string, error) {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Add a folder of documents to the agent's knowledge base",
	})
	if err != nil {
		return nil, err
	}
	if dir != "" {
		if _, err := core.AddAgentKnowledgeFiles(id, []string{dir}); err != nil {
			return nil, err
		}
	}
	return core.ListAgentKnowledge(id)
}

// DeleteAgentKnowledgeFile removes one knowledge file.
func (a *App) DeleteAgentKnowledgeFile(id, rel string) ([]string, error) {
	if err := core.DeleteAgentKnowledgeFile(id, rel); err != nil {
		return nil, err
	}
	return core.ListAgentKnowledge(id)
}

// backendEnvKey maps a graphify backend name to the env var that holds
// its API key. Empty backend == code-only (AST extraction, no key).
var backendEnvKey = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"gemini":    "GEMINI_API_KEY",
	"deepseek":  "DEEPSEEK_API_KEY",
	"kimi":      "MOONSHOT_API_KEY",
}

// BuildAgentRAG runs `graphify extract` over the knowledge folder so the
// index at knowledge/graphify-out is ready before the agent's first
// query. backend selects the semantic-extraction LLM ("" / "code" =
// code-only, which needs no key; documents/PDFs need a real backend +
// apiKey). Output streams over "praimate:install"; on failure the real
// graphify error is surfaced (not a bare "exit status 1").
func (a *App) BuildAgentRAG(id, backend, apiKey string) error {
	if _, err := exec.LookPath("graphify"); err != nil {
		return fmt.Errorf("graphify is not installed — install it from the CLIs tab (Managed tools) first")
	}
	dir, err := core.AgentKnowledgeDir(id)
	if err != nil {
		return err
	}
	if !dirExists(dir) {
		return fmt.Errorf("the agent has no knowledge documents yet — add files first")
	}

	args := []string{"extract", "."}
	env := os.Environ()
	if backend != "" && backend != "code" {
		args = append(args, "--backend", backend)
		if key := backendEnvKey[backend]; key != "" && strings.TrimSpace(apiKey) != "" {
			env = append(env, key+"="+strings.TrimSpace(apiKey))
		}
	}

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "graphify", args...)
	hideConsole(cmd)
	cmd.Dir = dir
	cmd.Env = env
	// Stream live AND keep a tail so the failure message is actionable.
	var tail tailBuffer
	w := io.MultiWriter(installLogWriter{ctx: a.ctx, cli: "graphify:" + id}, &tail)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(tail.String())
		hint := ""
		if strings.Contains(msg, "no LLM API key") || strings.Contains(msg, "requires") || strings.Contains(msg, "semantic") {
			hint = "\n\nDocument/PDF indexing needs an LLM backend + API key (pick one above). " +
				"Or switch this agent to Raw documents — that needs no key (the agent reads the files directly)."
		}
		if msg != "" {
			return fmt.Errorf("graphify extract failed:\n%s%s", lastLines(msg, 12), hint)
		}
		return fmt.Errorf("graphify extract failed: %w%s", err, hint)
	}
	return nil
}

// tailBuffer keeps only the last ~8KB written — enough for the error
// tail without unbounded memory on a chatty extract.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 8192 {
		t.b = t.b[len(t.b)-8192:]
	}
	return len(p), nil
}
func (t *tailBuffer) String() string { return string(t.b) }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ImportWorkpathTemplateDialog opens a folder picker and converts the
// chosen pre-1.1 workpath template (or a parent dir of templates) into
// agent(s) with their knowledge bases. Returns a short summary.
func (a *App) ImportWorkpathTemplateDialog() (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Import a workpath template folder (or a folder of templates)",
	})
	if err != nil || dir == "" {
		return "", err
	}
	if core.IsWorkpathTemplate(dir) {
		ag, err := c.ImportWorkpathTemplate(a.ctx, dir, "", nil)
		if err != nil {
			return "", err
		}
		return "Imported agent: " + ag.Name, nil
	}
	// Parent dir: import every template subdir (skip _common / dotdirs).
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		return "", rerr
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_common" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if !core.IsWorkpathTemplate(sub) {
			continue
		}
		if ag, err := c.ImportWorkpathTemplate(a.ctx, sub, "", nil); err == nil {
			names = append(names, ag.Name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no workpath templates found in %s", dir)
	}
	return fmt.Sprintf("Imported %d agent(s): %s", len(names), strings.Join(names, ", ")), nil
}

// ExportAgentPackDialog saves the agent as a .praimate-agent pack
// (yaml + knowledge, RAG index included) — or bare YAML when the user
// picks that extension.
func (a *App) ExportAgentPackDialog(id string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Export agent pack",
		DefaultFilename: id + core.AgentPackExt,
		Filters: []wruntime.FileFilter{
			{DisplayName: "PrAImate agent pack", Pattern: "*" + core.AgentPackExt},
			{DisplayName: "Agent YAML only", Pattern: "*.yaml;*.yml"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		return path, c.ExportAgent(a.ctx, id, path)
	}
	return path, c.ExportAgentPack(a.ctx, id, path)
}
