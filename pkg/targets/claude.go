package targets

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sPROFFEs/Clade/pkg/workpath"
)

// claudeTarget emits a Claude Code skill bundle.
//
// Layout produced under outDir:
//
//	.claude/skills/<name>/SKILL.md          (frontmatter: name, description)
//	.claude/skills/<name>/scripts/<tool>.sh (one per tool, mode preserved)
//	.claude/agents/<name>__<agent>.md       (one per subagent, namespaced)
//
// Skill names are sanitized to kebab-case (Claude Code's frontmatter
// validator rejects underscores).
type claudeTarget struct{}

func (claudeTarget) Name() string { return "claude" }

func (claudeTarget) Description() string {
	return "Claude Code skill (.claude/skills/<name>/SKILL.md + scripts + agents)"
}

func (claudeTarget) Compile(wp *workpath.Workpath, outDir string) error {
	skillName := kebab(wp.Name)
	skillDir := filepath.Join(outDir, ".claude", "skills", skillName)

	body := claudeSkillBody(wp)
	if err := writeFile(filepath.Join(skillDir, "SKILL.md"), body); err != nil {
		return err
	}

	for _, t := range wp.Tools {
		for _, scriptRel := range t.AllScripts() {
			src := filepath.Join(wp.SourceDir, filepath.FromSlash(scriptRel))
			dst := filepath.Join(skillDir, "scripts", filepath.Base(scriptRel))
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copy tool %s: %w", t.Name, err)
			}
		}
	}

	for _, a := range wp.Agents {
		src := filepath.Join(wp.SourceDir, filepath.FromSlash(a.Prompt))
		agentName := fmt.Sprintf("%s__%s", skillName, kebab(a.Name))
		dst := filepath.Join(outDir, ".claude", "agents", agentName+".md")
		body, err := claudeAgentBody(wp, a, src)
		if err != nil {
			return fmt.Errorf("render agent %s: %w", a.Name, err)
		}
		if err := writeFile(dst, body); err != nil {
			return err
		}
	}

	// Stage knowledge/ at the sandbox root so the agent's file-reading
	// tools find it at the same relative path the manifest in SKILL.md
	// advertises.
	if err := copyKnowledge(wp, outDir); err != nil {
		return err
	}

	return nil
}

func claudeSkillBody(wp *workpath.Workpath) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", kebab(wp.Name))
	fmt.Fprintf(&b, "description: %s\n", yamlString(wp.Description))
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", wp.Name)
	b.WriteString(renderMissionBlock(wp))
	b.WriteString(renderToolList(wp))
	b.WriteString(renderAgentList(wp))
	b.WriteString(renderKnowledgeBlock(wp))
	if len(wp.Tools) > 0 {
		b.WriteString("\nScripts live in `scripts/` next to this SKILL.md and can be invoked via the Bash tool.\n")
	}
	return b.String()
}

func claudeAgentBody(wp *workpath.Workpath, a workpath.Agent, promptPath string) (string, error) {
	prompt, err := readFile(promptPath)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s__%s\n", kebab(wp.Name), kebab(a.Name))
	if a.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlString(a.Description))
	}
	if len(a.Tools) > 0 {
		fmt.Fprintf(&b, "tools: [%s]\n", strings.Join(a.Tools, ", "))
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	return b.String(), nil
}

// kebab lowercases and replaces underscores/spaces with hyphens. Claude Code
// skill names must match ^[a-z0-9][a-z0-9-]*$.
func kebab(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// yamlString quotes a string for safe inclusion as a single-line YAML scalar.
// Quotes only when needed (presence of colon, leading/trailing space, etc.).
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#\"'\n") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
