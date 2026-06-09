package launcher

import (
	"fmt"
	"os"
	"path/filepath"
)

const managedOpenClaudeHomeName = ".openclaude-home"

func managedOpenClaudeHome(ws Workspace) string {
	return filepath.Join(ws.Root, managedOpenClaudeHomeName)
}

func openClaudeLocalLLMEnabled(settings WorkspaceSettings) bool {
	o := settings.Ollama
	return o.Endpoint != "" && o.Model != "" && o.HasAgent(AgentOpenClaude)
}

func openClaudeHomeForChat(c Chat) string {
	if openClaudeLocalLLMEnabled(c.Settings) {
		return filepath.Join(c.Root, managedOpenClaudeHomeName)
	}
	return homeDir()
}

func openClaudeHomeForWorkspace(ws Workspace) string {
	if openClaudeLocalLLMEnabled(ws.Settings) {
		return managedOpenClaudeHome(ws)
	}
	return homeDir()
}

func ensureManagedOpenClaudeHome(ws Workspace) (string, error) {
	home := managedOpenClaudeHome(ws)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", home, err)
	}
	if err := writeFileAtomic(filepath.Join(home, ".gitignore"), []byte("*\n!.gitignore\n"), 0o644); err != nil {
		return "", fmt.Errorf("write managed openclaude home .gitignore: %w", err)
	}
	for _, dir := range []string{".openclaude", ".claude"} {
		path := filepath.Join(home, dir)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", path, err)
		}
		cred := filepath.Join(path, ".credentials.json")
		if err := writeFileAtomic(cred, []byte("{}"), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", cred, err)
		}
	}
	return home, nil
}
