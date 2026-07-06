package targets

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sPROFFEs/PrAImate/pkg/workpath"
)

// geminiTarget emits a GEMINI.md at the out-dir root — the file Gemini CLI
// scans for project-specific instructions on every invocation, much like
// Codex's AGENTS.md or Claude's SKILL.md. Tool scripts and subagent
// prompts go into GEMINI.assets/ so the model can shell out to them by
// relative path.
//
// Layout produced under outDir:
//
//	GEMINI.md
//	GEMINI.assets/tools/<tool>.sh
//	GEMINI.assets/agents/<agent>.md
//
// Like codex, the file is always named GEMINI.md regardless of the
// workpath name — that's what the host CLI scans for. Overwrites any
// existing GEMINI.md.
type geminiTarget struct{}

func (geminiTarget) Name() string { return "gemini" }

func (geminiTarget) Description() string {
	return "GEMINI.md at out-dir root (Gemini CLI); assets in GEMINI.assets/"
}

func (geminiTarget) Compile(wp *workpath.Workpath, outDir string) error {
	mdPath := filepath.Join(outDir, "GEMINI.md")
	if err := writeFile(mdPath, geminiBody(wp)); err != nil {
		return err
	}
	// Knowledge always copies, even when there are no tools/agents.
	if err := copyKnowledge(wp, outDir); err != nil {
		return err
	}
	if len(wp.Tools) == 0 && len(wp.Agents) == 0 {
		return nil
	}
	assetsDir := filepath.Join(outDir, "GEMINI.assets")
	for _, t := range wp.Tools {
		for _, scriptRel := range t.AllScripts() {
			src := wp.ResolveToolScript(t, scriptRel)
			dst := filepath.Join(assetsDir, "tools", filepath.Base(scriptRel))
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copy tool %s: %w", t.Name, err)
			}
		}
	}
	for _, a := range wp.Agents {
		src := wp.ResolveAgentPrompt(a)
		dst := filepath.Join(assetsDir, "agents", filepath.Base(a.Prompt))
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy agent %s: %w", a.Name, err)
		}
	}
	return nil
}

func geminiBody(wp *workpath.Workpath) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", wp.Name)
	fmt.Fprintf(&b, "> %s\n\n", wp.Description)
	if wp.Version != "" {
		fmt.Fprintf(&b, "_Version %s", wp.Version)
		if wp.License != "" {
			fmt.Fprintf(&b, " · %s", wp.License)
		}
		b.WriteString("_\n\n")
	}
	b.WriteString(renderMissionBlock(wp))
	b.WriteString(renderToolList(wp))
	b.WriteString(renderAgentList(wp))
	b.WriteString(renderKnowledgeBlock(wp))
	b.WriteString(renderHookNote(wp, "gemini"))
	if len(wp.Tools) > 0 {
		b.WriteString("\nTool scripts live under `GEMINI.assets/tools/`. " +
			"Invoke with the shell tool using the relative path.\n")
	}
	if len(wp.Agents) > 0 {
		b.WriteString("\nSubagent prompts live under `GEMINI.assets/agents/`. " +
			"Read the file when you need to adopt that persona.\n")
	}
	return b.String()
}
