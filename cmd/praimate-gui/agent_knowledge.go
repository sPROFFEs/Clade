package main

// Agent knowledge bindings — the GUI side of knowledge packs: pick
// docs (files or a whole folder) into the agent's knowledge dir, set
// the mode (raw folder vs graphify RAG), build/refresh the RAG index
// with streamed output, and import/export .praimate-agent packs.

import (
	"context"
	"fmt"
	"os/exec"
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
		HasIndex:          dirExists(dir + "/.graphify"),
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

// BuildAgentRAG runs `graphify extract` over the knowledge folder so
// the index at knowledge/.graphify is ready before the agent's first
// query. Output streams over "praimate:install" (same channel the CLI
// installs use). Requires graphify on PATH — the frontend gates the
// button on GraphifyInstalled and points at the CLIs tab otherwise.
func (a *App) BuildAgentRAG(id string) error {
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
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "graphify", "extract")
	hideConsole(cmd)
	cmd.Dir = dir
	w := installLogWriter{ctx: a.ctx, cli: "graphify:" + id}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("graphify extract failed: %w", err)
	}
	return nil
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
