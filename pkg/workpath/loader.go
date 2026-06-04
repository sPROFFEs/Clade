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

	// Optional manifest. Imports are read here too — they're not applied by
	// applyManifest (which only touches direct fields) but by mergeImports
	// below, after the workpath's own knowledge / tools / agents are loaded
	// so the consumer's entries take precedence on collisions.
	var manifestImports []string
	manifestPath := filepath.Join(abs, "workpath.json")
	if raw, err := os.ReadFile(manifestPath); err == nil {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse workpath.json: %w", err)
		}
		applyManifest(wp, &m)
		manifestImports = m.Imports
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

	// Auto-discover knowledge files under knowledge/ (recursive).
	knowledge, err := discoverKnowledge(abs)
	if err != nil {
		return nil, err
	}
	wp.Knowledge = knowledge

	// Hooks (optional). Schema is a single hooks.json at the workpath
	// root with {"hooks": [...]}. Per-target emit is in pkg/targets/.
	hooks, err := loadHooks(abs)
	if err != nil {
		return nil, err
	}
	wp.Hooks = hooks

	// Resolve and merge imports (declared in workpath.json -> manifestImports).
	// Consumer's tools/agents/knowledge are loaded already, so collisions
	// during the merge are resolved in the consumer's favor.
	if len(manifestImports) > 0 {
		if err := mergeImports(wp, manifestImports); err != nil {
			return nil, err
		}
	}

	return wp, nil
}

// LoadImport reads a capability-bundle directory and returns its Import.
// Unlike LoadDir an Import has no mission requirement; only
// playbook-fragment.md / rules-fragment.md (both optional) and the
// usual tools/, agents/, knowledge/ subtrees are read.
//
// Returns an error if dir does not exist, or if tools / agents / knowledge
// discovery fails. An empty bundle (no fragments, no tools, no agents, no
// knowledge) is valid — though such an import is a no-op.
func LoadImport(dir string) (*Import, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve import dir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat import dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("import is not a directory: %s", abs)
	}

	imp := &Import{
		Name:      filepath.Base(abs),
		SourceDir: abs,
	}

	imp.PlaybookFragment, err = readTrimmed(abs, "playbook-fragment.md")
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}
	imp.RulesFragment, err = readTrimmed(abs, "rules-fragment.md")
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}

	imp.Tools, err = discoverTools(abs)
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}
	imp.Agents, err = discoverAgents(abs)
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}
	imp.Knowledge, err = discoverKnowledge(abs)
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}
	imp.Hooks, err = loadHooks(abs)
	if err != nil {
		return nil, fmt.Errorf("import %s: %w", imp.Name, err)
	}
	return imp, nil
}

// loadHooks reads hooks.json from dir. Missing file → nil (not an
// error); malformed JSON or invalid events → error. The loader does
// the validity check up front so authoring mistakes fail at compile
// time, not at agent-launch time.
func loadHooks(dir string) ([]Hook, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "hooks.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hooks.json: %w", err)
	}
	var m HooksManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse hooks.json: %w", err)
	}
	for i, h := range m.Hooks {
		if !AllHookEvents[h.Event] {
			return nil, fmt.Errorf("hooks[%d]: unknown event %q (valid: pre_tool, post_tool, user_input, session_start, session_stop, subagent_stop, notification)", i, h.Event)
		}
		if strings.TrimSpace(h.Command) == "" {
			return nil, fmt.Errorf("hooks[%d] (%s): command is required", i, h.Event)
		}
	}
	return m.Hooks, nil
}

// mergeImports resolves each path in manifestImports relative to the
// PARENT of wp.SourceDir (the "templates root") and merges its tools,
// agents, knowledge, and playbook/rules fragments into wp.
//
// Collision policy: the consumer (wp) always wins. An imported tool /
// agent whose Name already exists on wp is skipped silently; an imported
// knowledge file whose RelPath already exists on wp is skipped silently.
// Fragment text is always appended — the consumer's playbook/rules
// remain at the top, with imported fragments below under headings.
//
// Absolute import paths are honored as-is (useful for tests). Missing
// imports produce a hard error so a typo in workpath.json doesn't
// silently degrade the resulting workpath.
func mergeImports(wp *Workpath, manifestImports []string) error {
	templatesRoot := filepath.Dir(wp.SourceDir)
	// Stable key sets for collision checks.
	existingTools := map[string]bool{}
	for _, t := range wp.Tools {
		existingTools[t.Name] = true
	}
	existingAgents := map[string]bool{}
	for _, a := range wp.Agents {
		existingAgents[a.Name] = true
	}
	existingKnowledge := map[string]bool{}
	for _, k := range wp.Knowledge {
		existingKnowledge[k.RelPath] = true
	}
	// Hook collision key is event+matcher so a consumer can override
	// the imported "pre_tool/Bash" hook without dropping an imported
	// "pre_tool/Edit" hook.
	hookKey := func(h Hook) string { return string(h.Event) + "|" + h.Matcher }
	existingHooks := map[string]bool{}
	for _, h := range wp.Hooks {
		existingHooks[hookKey(h)] = true
	}

	for _, raw := range manifestImports {
		path := raw
		if !filepath.IsAbs(path) {
			path = filepath.Join(templatesRoot, filepath.FromSlash(raw))
		}
		imp, err := LoadImport(path)
		if err != nil {
			return fmt.Errorf("import %q: %w", raw, err)
		}

		for _, t := range imp.Tools {
			if existingTools[t.Name] {
				continue // consumer wins
			}
			t.ImportedFrom = imp.SourceDir
			wp.Tools = append(wp.Tools, t)
			existingTools[t.Name] = true
		}
		for _, a := range imp.Agents {
			if existingAgents[a.Name] {
				continue
			}
			a.ImportedFrom = imp.SourceDir
			wp.Agents = append(wp.Agents, a)
			existingAgents[a.Name] = true
		}
		for _, k := range imp.Knowledge {
			if existingKnowledge[k.RelPath] {
				continue
			}
			k.ImportedFrom = imp.SourceDir
			wp.Knowledge = append(wp.Knowledge, k)
			existingKnowledge[k.RelPath] = true
		}
		for _, h := range imp.Hooks {
			if existingHooks[hookKey(h)] {
				continue
			}
			h.ImportedFrom = imp.SourceDir
			wp.Hooks = append(wp.Hooks, h)
			existingHooks[hookKey(h)] = true
		}

		// Append fragments under named sub-headings so the agent sees
		// where each rule/stage comes from. Empty fragments are no-ops.
		if imp.PlaybookFragment != "" {
			if wp.Playbook != "" {
				wp.Playbook += "\n\n"
			}
			wp.Playbook += "## Imported capabilities: " + imp.Name + "\n\n" + imp.PlaybookFragment
		}
		if imp.RulesFragment != "" {
			if wp.Rules != "" {
				wp.Rules += "\n\n"
			}
			wp.Rules += "## Imported rules: " + imp.Name + "\n\n" + imp.RulesFragment
		}
	}

	// Re-sort the appended slices so the manifest renders deterministically.
	sort.Slice(wp.Tools, func(i, j int) bool { return wp.Tools[i].Name < wp.Tools[j].Name })
	sort.Slice(wp.Agents, func(i, j int) bool { return wp.Agents[i].Name < wp.Agents[j].Name })
	sort.Slice(wp.Knowledge, func(i, j int) bool { return wp.Knowledge[i].RelPath < wp.Knowledge[j].RelPath })
	return nil
}

// textExts is the set of file extensions we treat as AI-legible
// during knowledge discovery — used to decide whether to extract a
// summary preview vs just list by name. Lowercase, leading dot.
var textExts = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true,
	".txt": true, ".text": true,
	".rst":  true,
	".org":  true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".csv": true,
	".log": true,
}

// discoverKnowledge walks <root>/knowledge/ recursively and returns
// one KnowledgeFile per regular file. Hidden files (leading ".") and
// dirs are skipped. Order is deterministic (sorted by RelPath) so
// the compiled manifest is stable across runs.
func discoverKnowledge(root string) ([]KnowledgeFile, error) {
	base := filepath.Join(root, "knowledge")
	info, err := os.Stat(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat knowledge/: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("knowledge/ exists but is not a directory")
	}

	var out []KnowledgeFile
	err = filepath.Walk(base, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Hide dot-files / dot-dirs (.git, .DS_Store, …).
		name := fi.Name()
		if strings.HasPrefix(name, ".") {
			if fi.IsDir() && path != base {
				return filepath.SkipDir
			}
			if !fi.IsDir() {
				return nil
			}
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(filepath.Ext(path))
		isText := textExts[ext]
		k := KnowledgeFile{
			RelPath: rel,
			Title:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Bytes:   fi.Size(),
			IsText:  isText,
		}
		if isText {
			title, summary := summariseTextFile(path)
			if title != "" {
				k.Title = title
			}
			k.Summary = summary
		}
		out = append(out, k)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk knowledge/: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// summariseTextFile reads the first ~4KB of a text knowledge file
// and pulls out (1) an H1 heading or first non-blank line as the
// title, and (2) the first non-trivial paragraph as the summary
// (capped at ~280 chars so the manifest stays readable). Failures
// are silent — title falls back to the filename downstream.
func summariseTextFile(path string) (title, summary string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	const max = 4096
	buf := make([]byte, max)
	n, _ := f.Read(buf)
	body := string(buf[:n])

	// Strip a UTF-8 BOM (EF BB BF) if present so first-line detection
	// works. Written as an explicit byte sequence because a bare
	// U+FEFF in this source file would itself be a mid-file BOM, which
	// Go forbids.
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")

	lines := strings.Split(body, "\n")
	var firstHeading, firstPara string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			if firstPara != "" {
				break // paragraph complete
			}
			continue
		}
		// H1 candidate: "# Title" markdown or all-caps first line.
		if firstHeading == "" && strings.HasPrefix(t, "# ") {
			firstHeading = strings.TrimSpace(strings.TrimPrefix(t, "#"))
			continue
		}
		if firstPara == "" {
			firstPara = t
			continue
		}
		// Continue collecting the paragraph until we hit a blank line.
		firstPara += " " + t
	}
	if firstHeading == "" {
		// Fall back to first non-blank line.
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t != "" {
				firstHeading = t
				if strings.HasPrefix(firstHeading, "# ") {
					firstHeading = strings.TrimSpace(strings.TrimPrefix(firstHeading, "#"))
				}
				break
			}
		}
	}
	// Cap summary length so manifests don't blow up. We trim on a
	// word boundary when possible.
	if len(firstPara) > 280 {
		cut := 280
		if sp := strings.LastIndexByte(firstPara[:cut], ' '); sp > 200 {
			cut = sp
		}
		firstPara = firstPara[:cut] + "…"
	}
	return firstHeading, firstPara
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
