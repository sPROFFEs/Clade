package core

// Workpath-template → agent migration. The pre-1.1 Clade model used
// on-disk "workpath templates" (mission/personality/playbook/rules +
// knowledge/ + tools/ + agents/), compiled per-chat by wpc. The 1.1
// model is DB-backed agents with a portable knowledge folder. This
// bridges the two: it reads a workpath template directory and produces
// an agent whose instructions are the composed system prompt and whose
// knowledge base is the template's knowledge/tools/agents content.
//
// The mapping:
//   personality.md   → persona preamble (the leading HTML comment that
//                      documents the file format is stripped)
//   mission.md       → the agent's purpose
//   playbook.md      → step-by-step procedures
//   rules.md         → hard constraints
//   knowledge/       → copied verbatim into the agent's knowledge dir
//   tools/           → copied under knowledge/tools (scripts the agent
//                      reads + runs)
//   agents/          → copied under knowledge/subagents (sub-personas)
//
// Knowledge mode defaults to "raw": the documents are mixed Markdown +
// binaries, which the wrapped CLIs read directly with their file tools.
// (graphify RAG only helps for large code corpora and needs an API key.)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// workpathJSON is the subset of a template's workpath.json we read.
type workpathJSON struct {
	Description string `json:"description"`
}

// templateRoot returns the directory that actually holds the template
// files. Templates use one of two layouts: flat (mission.md etc. at
// dir) or nested (the real files under dir/workpath/, with dir being a
// named wrapper). Returns the resolved root and whether dir is a
// template at all.
func templateRoot(dir string) (string, bool) {
	hasFiles := func(d string) bool {
		for _, f := range []string{"workpath.json", "mission.md"} {
			if _, err := os.Stat(filepath.Join(d, f)); err == nil {
				return true
			}
		}
		return false
	}
	if hasFiles(dir) {
		return dir, true
	}
	nested := filepath.Join(dir, "workpath")
	if hasFiles(nested) {
		return nested, true
	}
	return "", false
}

// IsWorkpathTemplate reports whether dir is a workpath template (flat or
// nested layout). Used by the GUI to validate a picked folder.
func IsWorkpathTemplate(dir string) bool {
	_, ok := templateRoot(dir)
	return ok
}

// personaCommentRE strips the leading HTML comment block that documents
// the persona-file format (everything up to and including the first
// `-->`), leaving only the actual persona text.
var personaCommentRE = regexp.MustCompile(`(?s)^\s*<!--.*?-->\s*`)

// ImportWorkpathTemplate creates an agent from a workpath template
// directory and populates its knowledge base. Returns the new agent.
// idOverride, when non-empty, sets the agent id (else the directory
// basename is used). supports, when non-empty, sets the CLI list (else
// a sensible default).
func (c *Core) ImportWorkpathTemplate(ctx context.Context, dir, idOverride string, supports []string) (*Agent, error) {
	root, ok := templateRoot(dir)
	if !ok {
		return nil, fmt.Errorf("%s is not a workpath template (no workpath.json or mission.md)", dir)
	}
	// id/name come from the OUTER dir name so nested-layout templates
	// (clade-dev/workpath/) don't all collide on the id "workpath".
	base := filepath.Base(strings.TrimRight(dir, string(filepath.Separator)))
	id := idOverride
	if id == "" {
		id = sanitizeAgentID(base)
	}

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}

	var wj workpathJSON
	if raw, err := os.ReadFile(filepath.Join(root, "workpath.json")); err == nil {
		_ = json.Unmarshal(raw, &wj)
	}

	persona := personaCommentRE.ReplaceAllString(read("personality.md"), "")
	mission := read("mission.md")
	playbook := read("playbook.md")
	rules := read("rules.md")

	var b strings.Builder
	if persona != "" {
		b.WriteString(persona)
		b.WriteString("\n\n")
	}
	if mission != "" {
		b.WriteString(mission)
		b.WriteString("\n\n")
	}
	if playbook != "" {
		b.WriteString("# Playbook\n\n")
		b.WriteString(playbook)
		b.WriteString("\n\n")
	}
	if rules != "" {
		b.WriteString("# Rules\n\n")
		b.WriteString(rules)
		b.WriteString("\n\n")
	}
	// Point the agent at the helper scripts and sub-agent personas that
	// travel in its knowledge base.
	hasTools := dirExistsAt(root, "tools")
	hasSubagents := dirExistsAt(root, "agents")
	if hasTools || hasSubagents {
		b.WriteString("# Helper assets\n\n")
		if hasTools {
			b.WriteString("Reusable shell scripts live in your knowledge base under `tools/`. " +
				"Read a script before running it, then invoke it from the working directory.\n")
		}
		if hasSubagents {
			b.WriteString("Sub-agent role descriptions live under `subagents/` — consult them when a task " +
				"matches one of those specialisations.\n")
		}
		b.WriteString("\n")
	}
	instructions := strings.TrimSpace(b.String())
	if instructions == "" {
		instructions = "You are " + humanizeID(base) + "."
	}

	desc := wj.Description
	if desc == "" {
		desc = "Imported from the " + base + " workpath template."
	}

	if supports == nil {
		// These templates lean on bash tools + native context files,
		// which claude/openclaude/codex/opencode all honour.
		supports = []string{"claude", "openclaude", "codex", "opencode"}
	}

	agent := &Agent{
		ID:           id,
		Name:         humanizeID(base),
		Description:  firstLine(desc),
		Instructions: instructions,
		Supports:     supports,
		Knowledge:    "raw",
	}
	if _, err := c.upsertAgent(ctx, agent); err != nil {
		return nil, err
	}

	// Populate the knowledge base: knowledge/ verbatim, tools/ →
	// knowledge/tools, agents/ → knowledge/subagents.
	dst, err := AgentKnowledgeDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	for src, sub := range map[string]string{"knowledge": ".", "tools": "tools", "agents": "subagents"} {
		from := filepath.Join(root, src)
		if !dirExists(from) {
			continue
		}
		to := dst
		if sub != "." {
			to = filepath.Join(dst, sub)
		}
		if err := copyTree(from, to); err != nil {
			return nil, fmt.Errorf("copy %s: %w", src, err)
		}
	}
	return c.GetAgent(ctx, id)
}

func dirExistsAt(parent, name string) bool { return dirExists(filepath.Join(parent, name)) }

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// copyTree recursively copies src into dst, creating dirs as needed.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyOneFile(p, target)
	})
}

func copyOneFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	// Preserve the executable bit so tools/ scripts stay runnable.
	if fi, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, fi.Mode().Perm())
	}
	return out.Close()
}

var agentIDInvalidRE = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeAgentID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = agentIDInvalidRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func humanizeID(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
