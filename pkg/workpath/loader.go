package workpath

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadDir reads a workpath directory and returns the parsed Workpath.
//
// The directory name is used as the default Name. workpath.json (if present)
// can override any field. Tools and agents are auto-discovered from tools/
// and agents/ unless the manifest provides explicit overrides.
//
// Returns an error if the directory is missing, mission.md is absent or empty,
// or the manifest is malformed.
func LoadDir(dir string) (*Workpath, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve source dir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}

	wp := &Workpath{
		Name:      filepath.Base(abs),
		Version:   "1",
		SourceDir: abs,
	}

	// Optional manifest.
	manifestPath := filepath.Join(abs, "workpath.json")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse workpath.json: %w", err)
		}
		applyManifest(wp, &m)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read workpath.json: %w", err)
	}

	// Required: mission.md (with module.md as a back-compat alias for the
	// mika-code directory convention).
	mission, err := readOptional(abs, "mission.md")
	if err != nil {
		return nil, err
	}
	if mission == "" {
		mission, err = readOptional(abs, "module.md")
		if err != nil {
			return nil, err
		}
	}
	wp.Mission = strings.TrimSpace(mission)

	// If the manifest didn't supply a description, derive one from the first
	// non-heading, non-blockquote line of the mission body. This keeps
	// hand-authored mika dirs valid as wpc sources without forcing an extra
	// file.
	if wp.Description == "" {
		wp.Description = deriveDescription(wp.Mission)
	}

	// Optional bodies.
	wp.Playbook, err = readTrimmed(abs, "playbook.md")
	if err != nil {
		return nil, err
	}
	wp.Rules, err = readTrimmed(abs, "rules.md")
	if err != nil {
		return nil, err
	}

	// Auto-discover tools if manifest didn't supply any.
	if len(wp.Tools) == 0 {
		tools, err := discoverTools(abs)
		if err != nil {
			return nil, err
		}
		wp.Tools = tools
	}

	// Auto-discover agents if manifest didn't supply any.
	if len(wp.Agents) == 0 {
		agents, err := discoverAgents(abs)
		if err != nil {
			return nil, err
		}
		wp.Agents = agents
	}

	return wp, nil
}

func applyManifest(wp *Workpath, m *Manifest) {
	if m.Name != "" {
		wp.Name = m.Name
	}
	if m.Description != "" {
		wp.Description = m.Description
	}
	if m.Version != "" {
		wp.Version = m.Version
	}
	if m.License != "" {
		wp.License = m.License
	}
	for _, t := range m.Tools {
		wp.Tools = append(wp.Tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			Script:      t.Script,
			Shell:       inferShell(t.Script),
		})
	}
	for _, a := range m.Agents {
		wp.Agents = append(wp.Agents, Agent{
			Name:        a.Name,
			Description: a.Description,
			Prompt:      a.Prompt,
			Tools:       a.Tools,
		})
	}
}

func readOptional(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(raw), nil
}

func readTrimmed(dir, name string) (string, error) {
	s, err := readOptional(dir, name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func discoverTools(dir string) ([]Tool, error) {
	toolsDir := filepath.Join(dir, "tools")
	entries, err := os.ReadDir(toolsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tools/: %w", err)
	}

	// Group by basename so platform-paired scripts (foo.sh + foo.ps1) end
	// up as a single Tool with two script files. Without this the
	// validator complains about duplicate tool names.
	type group struct {
		name        string
		description string
		scripts     []string // relative paths
	}
	groups := map[string]*group{}
	var order []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fname := e.Name()
		ext := strings.ToLower(filepath.Ext(fname))
		if ext != ".sh" && ext != ".ps1" {
			continue
		}
		base := strings.TrimSuffix(fname, ext)
		rel := filepath.ToSlash(filepath.Join("tools", fname))
		path := filepath.Join(toolsDir, fname)
		desc := firstCommentLine(path)

		g, ok := groups[base]
		if !ok {
			g = &group{name: base}
			groups[base] = g
			order = append(order, base)
		}
		g.scripts = append(g.scripts, rel)
		// First non-empty description wins — keeps the author in control
		// when they document only one of the two scripts.
		if g.description == "" {
			g.description = desc
		}
	}

	tools := make([]Tool, 0, len(groups))
	for _, base := range order {
		g := groups[base]
		// Prefer .sh as the "primary" script so Shell defaults to bash;
		// .ps1 is the Windows fallback that targets also copy.
		sort.Slice(g.scripts, func(i, j int) bool {
			return scriptPriority(g.scripts[i]) < scriptPriority(g.scripts[j])
		})
		primary := g.scripts[0]
		tools = append(tools, Tool{
			Name:        base,
			Description: g.description,
			Script:      primary,
			Scripts:     g.scripts,
			Shell:       inferShell(primary),
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// scriptPriority orders script paths so .sh wins over .ps1 wins over
// anything else when picking a tool's primary script.
func scriptPriority(s string) int {
	switch strings.ToLower(filepath.Ext(s)) {
	case ".sh":
		return 0
	case ".ps1":
		return 1
	default:
		return 2
	}
}

func discoverAgents(dir string) ([]Agent, error) {
	agentsDir := filepath.Join(dir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agents/: %w", err)
	}
	var agents []Agent
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.ToLower(filepath.Ext(name)) != ".md" {
			continue
		}
		path := filepath.Join(agentsDir, name)
		desc := firstHeadingOrLine(path)
		agents = append(agents, Agent{
			Name:        strings.TrimSuffix(name, filepath.Ext(name)),
			Description: desc,
			Prompt:      filepath.ToSlash(filepath.Join("agents", name)),
		})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

// deriveDescription returns a one-line summary inferred from a markdown body.
// It walks lines, skipping structural ones (headings, blockquotes, bullets,
// fences, horizontal rules), and joins consecutive content lines until it
// finds a sentence boundary ("." followed by space or EOL). Returns "" if no
// content paragraph exists — Validate will then complain.
func deriveDescription(body string) string {
	var buf strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if buf.Len() > 0 {
				break // paragraph ended without a sentence terminator; use what we have
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, ">") ||
			strings.HasPrefix(trimmed, "- ") ||
			strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "```") ||
			strings.HasPrefix(trimmed, "---") {
			if buf.Len() > 0 {
				break
			}
			continue
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(trimmed)
		// Stop at the first sentence boundary.
		if idx := strings.Index(buf.String(), ". "); idx > 0 {
			return strings.TrimSpace(buf.String()[:idx+1])
		}
		if strings.HasSuffix(trimmed, ".") {
			return strings.TrimSpace(buf.String())
		}
	}
	return strings.TrimSpace(buf.String())
}

func inferShell(script string) string {
	switch strings.ToLower(filepath.Ext(script)) {
	case ".ps1":
		return "pwsh"
	default:
		return "bash"
	}
}

// firstCommentLine reads the first non-shebang `# ...` comment from a shell
// script and returns its text. Empty string on error or absence.
func firstCommentLine(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
		return ""
	}
	return ""
}

// firstHeadingOrLine returns the first H1 heading text, or the first non-empty
// line stripped of leading "# " markers. Used as an agent's auto-discovered
// description.
func firstHeadingOrLine(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
		if strings.HasPrefix(line, "---") {
			continue // skip frontmatter fence
		}
		return line
	}
	return ""
}
