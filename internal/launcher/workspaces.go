package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Workspace is one entry under <workspacesRoot>. Each workspace bundles a
// workpath (the wpc source — the knowledge base) and a sandbox (the
// agent's cwd — where it does its work). They are deliberately separated
// so the agent never modifies the knowledge base by accident.
type Workspace struct {
	Name        string            // dir name, must match workpath name rules
	Root        string            // <workspacesRoot>/<Name>
	WorkpathDir string            // <Root>/workpath
	SandboxDir  string            // <Root>/sandbox
	ChatsDir    string            // <Root>/chats  (reserved for Phase 2)
	Description string            // pulled from workpath.json or mission.md
	Settings    WorkspaceSettings // <Root>/workspace.json
}

// WorkspaceSettings are per-workspace knobs the TUI exposes.
type WorkspaceSettings struct {
	// Language, if set, gets prepended to the agent's initial context so
	// it consistently replies in that language (e.g. "es", "ja", "Italian").
	Language string `json:"language,omitempty"`
	// OnlineSkills is a list of URLs (git repos or zip archives) the
	// launcher fetches into the sandbox's .claude/skills/ on launch.
	OnlineSkills []string `json:"onlineSkills,omitempty"`
	// MemoryEnabled, when true, ensures the workspace has a MEMORY.md
	// file the agent can read/write across sessions.
	MemoryEnabled bool `json:"memoryEnabled,omitempty"`
	// Ollama, when populated, records an OpenAI-compatible local route for
	// supported agents. Claude Code remains on Anthropic; OpenClaude owns
	// the Claude-style local path. Stored here so the choice follows the workspace.
	Ollama OllamaSettings `json:"ollama,omitempty"`
	// MirrorAgentState, when true, opts the chat into Step 3's
	// mirror-in / mirror-out semantics: before launch the chat's
	// captured per-agent slice replaces the agent's home-dir view of
	// this cwd; after exit the home-dir view is mirrored back. Off by
	// default because the data-loss failure modes (SIGKILL between
	// agent exit and mirror-back, concurrent agent instances) are
	// real. Step 2's snapshot-on-exit always runs regardless; this
	// flag only controls whether we ALSO mirror IN at launch time.
	MirrorAgentState bool `json:"mirrorAgentState,omitempty"`
	// DisableContextPrimer, when true, suppresses the Option-C
	// fallback prompt that the launcher passes as the agent's
	// first positional argument on fresh launches ("read MEMORY.md,
	// playbook, rules; reply 'Context loaded'"). The primer is ON
	// by default; this field exists so users can turn it off per
	// chat from the settings menu when the agent's own auto-load
	// is enough and the primer would just add noise.
	DisableContextPrimer bool `json:"disableContextPrimer,omitempty"`
}

// OllamaSettings duplicates internal/ollama.Settings to avoid an import
// cycle (ollama depends on this package's notion of a workspace via
// callers in cmd/clade; keeping these types separate keeps the
// dependency graph one-way).
type OllamaSettings struct {
	Endpoint      string `json:"endpoint,omitempty"`
	Model         string `json:"model,omitempty"`
	WireAPI       string `json:"wireApi,omitempty"`
	APIKey        string `json:"apiKey,omitempty"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	OutputTokens  int    `json:"outputTokens,omitempty"`

	// Agents lists which agent IDs are opted into routing through
	// this endpoint at launch time. Each agent's Plan() branch
	// checks `HasAgent(id)` to decide whether to inject env vars /
	// CLI flags. Tracked separately from Endpoint/Model so the
	// chat-level settings can record "this chat is configured for a
	// local endpoint" (visible in the settings menu) independently
	// of which agents Plan() should actively route.
	//
	// Empty/missing defaults to ["claude"] for backward compat:
	// chats created before this field existed had chat-level Ollama
	// settings saved ONLY when claude was ticked (the old wizard
	// gated the write on claude), so an existing chat-level Ollama
	// block implies the user wanted claude routed.
	Agents []string `json:"agents,omitempty"`
}

// HasAgent reports whether the given agent ID is opted into Ollama
// routing for this chat. Used by Plan() to decide injection.
//
// Empty Agents defaults to {AgentClaude} — see field doc above.
func (s OllamaSettings) HasAgent(id AgentID) bool {
	if s.Endpoint == "" || s.Model == "" {
		return false
	}
	if len(s.Agents) == 0 {
		return id == AgentClaude
	}
	for _, a := range s.Agents {
		if a == string(id) {
			return true
		}
	}
	return false
}

// SaveWorkspaceSettings persists ws.Settings to <ws.Root>/workspace.json.
// Atomic (tmp + rename). Prefer SaveWorkspaceLikeSettings — it routes to
// chat.json or template.json when the Workspace originated from one.
func SaveWorkspaceSettings(ws Workspace) error {
	raw, err := json.MarshalIndent(ws.Settings, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := filepath.Join(ws.Root, "workspace.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveWorkspaceLikeSettings inspects ws.Root and persists settings to the
// canonical manifest for whatever it represents:
//
//	<root>/chats/<id>/      → patches chat.json (via SaveChatSettings)
//	<root>/templates/<name>/→ patches template.json (via SaveTemplateSettings)
//	<root>/<name>/          → legacy workspace.json
//
// Used by screens (Ollama, settings) that take a generic Workspace but
// need the write to land in the right place.
func SaveWorkspaceLikeSettings(ws Workspace) error {
	parent := filepath.Base(filepath.Dir(ws.Root))
	switch parent {
	case ChatsDir:
		root := filepath.Dir(filepath.Dir(ws.Root))
		id := filepath.Base(ws.Root)
		chat, err := LoadChat(root, id)
		if err != nil {
			return fmt.Errorf("load chat %s for settings save: %w", id, err)
		}
		if chat == nil {
			return fmt.Errorf("chat not found for settings save: %s", ws.Root)
		}
		chat.Settings = ws.Settings
		return SaveChatSettings(*chat)
	case TemplatesDir:
		root := filepath.Dir(filepath.Dir(ws.Root))
		name := filepath.Base(ws.Root)
		tpl, err := LoadTemplate(root, name)
		if err != nil {
			return fmt.Errorf("load template %s for settings save: %w", name, err)
		}
		if tpl == nil {
			return fmt.Errorf("template not found for settings save: %s", ws.Root)
		}
		tpl.Settings = ws.Settings
		return SaveTemplateSettings(*tpl)
	default:
		return SaveWorkspaceSettings(ws)
	}
}

type workpathManifest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

var workspaceNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidateName returns nil if name is a legal workspace / template
// identifier (matches workspaceNameRE), otherwise an error explaining
// the rule. Exported so TUI screens can validate as the user types
// instead of letting CreateTemplate/CreateWorkspace fail later and
// bubble a fatal error up to main.
func ValidateName(name string) error {
	if !workspaceNameRE.MatchString(name) {
		return fmt.Errorf("name must match [a-z0-9][a-z0-9_-]*, got %q", name)
	}
	return nil
}

// ListWorkspaces returns every workspace under root, sorted by name.
// Hidden dirs (leading dot) and dirs without a workpath/ subdir are
// silently skipped — that's how the picker behaves too.
func ListWorkspaces(root string) ([]Workspace, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Workspace
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		ws, err := LoadWorkspace(root, e.Name())
		if err != nil {
			// One broken workspace shouldn't break the whole list. Skip.
			continue
		}
		if ws != nil {
			out = append(out, *ws)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadWorkspace returns nil (without error) when the directory exists but
// has no workpath/ subdir — that's how we ignore stray dirs the user put
// next to real workspaces.
func LoadWorkspace(root, name string) (*Workspace, error) {
	wsRoot := filepath.Join(root, name)
	wpDir := filepath.Join(wsRoot, "workpath")
	info, err := os.Stat(wpDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	manifest, _ := readManifest(filepath.Join(wpDir, "workpath.json"))
	settings, _ := readSettings(filepath.Join(wsRoot, "workspace.json"))
	desc := manifest.Description
	if desc == "" {
		desc = firstParagraph(filepath.Join(wpDir, "mission.md"))
	}

	return &Workspace{
		Name:        name,
		Root:        wsRoot,
		WorkpathDir: wpDir,
		SandboxDir:  filepath.Join(wsRoot, "sandbox"),
		ChatsDir:    filepath.Join(wsRoot, "chats"),
		Description: desc,
		Settings:    settings,
	}, nil
}

func readManifest(path string) (workpathManifest, error) {
	var m workpathManifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal(raw, &m)
	return m, nil
}

func readSettings(path string) (WorkspaceSettings, error) {
	var s WorkspaceSettings
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	_ = json.Unmarshal(raw, &s)
	return s, nil
}

func firstParagraph(missionPath string) string {
	raw, err := os.ReadFile(missionPath)
	if err != nil {
		return ""
	}
	text := string(raw)
	for _, para := range strings.Split(text, "\n\n") {
		p := strings.TrimSpace(para)
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, "# ")
		p = strings.ReplaceAll(p, "\n", " ")
		if len(p) > 140 {
			p = p[:140] + "…"
		}
		return strings.TrimSpace(p)
	}
	return ""
}

// EnsureSandbox creates <Root>/sandbox the first time and drops a
// .gitignore so the agent's generated files don't pollute the workspace's
// git history if the user puts it under version control. Bails early
// with an actionable error if SandboxDir is empty — that signals a
// corrupt manifest, not a recoverable filesystem condition.
func EnsureSandbox(ws Workspace) error {
	if ws.SandboxDir == "" {
		return fmt.Errorf("workspace %q has an empty sandbox path — chat.json or workspace state is corrupt; delete the chat dir or re-create it", ws.Name)
	}
	if err := os.MkdirAll(ws.SandboxDir, 0o755); err != nil {
		return fmt.Errorf("create sandbox %s: %w", ws.SandboxDir, err)
	}
	gi := filepath.Join(ws.SandboxDir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(gi, []byte("*\n!.gitignore\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CreateWorkspace scaffolds a new workspace with a workpath/ that wpc
// will validate. Phase 1 only takes name + description; the richer
// wizard (memory, language, online skills) is Phase 2.
func CreateWorkspace(root, name, description string) (Workspace, error) {
	if !workspaceNameRE.MatchString(name) {
		return Workspace{}, fmt.Errorf("name must match [a-z0-9][a-z0-9_-]*, got %q", name)
	}
	wsRoot := filepath.Join(root, name)
	if _, err := os.Stat(wsRoot); err == nil {
		return Workspace{}, fmt.Errorf("workspace already exists: %s", wsRoot)
	}
	wpDir := filepath.Join(wsRoot, "workpath")
	if err := os.MkdirAll(filepath.Join(wpDir, "tools"), 0o755); err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(filepath.Join(wpDir, "agents"), 0o755); err != nil {
		return Workspace{}, err
	}

	manifest := workpathManifest{Description: description}
	manifestRaw, _ := json.MarshalIndent(struct {
		Description string `json:"description"`
		Version     string `json:"version"`
	}{description, "1"}, "", "  ")
	if err := os.WriteFile(filepath.Join(wpDir, "workpath.json"), append(manifestRaw, '\n'), 0o644); err != nil {
		return Workspace{}, err
	}

	mission := fmt.Sprintf("# %s\n\n%s\n\nDescribe the mission this workpath exists to accomplish — the desired outcome, the inputs the agent will receive, and the artifacts it should produce.\n", name, description)
	if err := os.WriteFile(filepath.Join(wpDir, "mission.md"), []byte(mission), 0o644); err != nil {
		return Workspace{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "playbook.md"),
		[]byte("## Stage 1 — Scout\n\n- (describe step)\n\n## Stage 2 — Execute\n\n- (describe step)\n"), 0o644); err != nil {
		return Workspace{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "rules.md"),
		[]byte("- Never (hard constraint)\n- Always (hard constraint)\n"), 0o644); err != nil {
		return Workspace{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "tools", ".gitkeep"), nil, 0o644); err != nil {
		return Workspace{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "agents", ".gitkeep"), nil, 0o644); err != nil {
		return Workspace{}, err
	}

	ws := Workspace{
		Name:        name,
		Root:        wsRoot,
		WorkpathDir: wpDir,
		SandboxDir:  filepath.Join(wsRoot, "sandbox"),
		ChatsDir:    filepath.Join(wsRoot, "chats"),
		Description: manifest.Description,
	}
	if err := EnsureSandbox(ws); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

// SeedSamples copies the bundled workpaths into workspacesRoot the first
// time the user sets it up. Existing workspaces are never overwritten.
// Returns the names of the workspaces actually seeded.
func SeedSamples(workspacesRoot string, sampleSourceCandidates []string) ([]string, error) {
	src := ""
	for _, c := range sampleSourceCandidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			src = c
			break
		}
	}
	if src == "" {
		return nil, nil
	}
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var seeded []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "_common" {
			if err := seedCommonBundles(filepath.Join(src, e.Name()), filepath.Join(workspacesRoot, TemplatesDir, "_common")); err != nil {
				return seeded, err
			}
			continue
		}
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		// New layout: seed samples as templates under <root>/templates/<name>/.
		dst := filepath.Join(workspacesRoot, TemplatesDir, e.Name(), "workpath")
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyTree(filepath.Join(src, e.Name()), dst); err != nil {
			return seeded, fmt.Errorf("seed %s: %w", e.Name(), err)
		}
		seeded = append(seeded, e.Name())
	}
	return seeded, nil
}

func seedCommonBundles(src, dstRoot string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		dst := filepath.Join(dstRoot, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyTree(filepath.Join(src, e.Name()), dst); err != nil {
			return fmt.Errorf("seed bundle %s: %w", e.Name(), err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// SampleCandidates returns the locations the launcher should check for
// bundled sample workpaths, in order. The first existing directory wins.
// We hand back several because the binary could be running from the repo
// root, a dist/ subdir, a global install location, etc.
func SampleCandidates(execDir string) []string {
	return []string{
		filepath.Join(execDir, "samples", "workpaths"),
		filepath.Join(execDir, "..", "samples", "workpaths"),
		filepath.Join(execDir, "..", "..", "samples", "workpaths"),
		filepath.Join(execDir, "..", "share", "praimate", "samples", "workpaths"),
	}
}

// SampleAgentCandidates mirrors SampleCandidates for the curated sample
// agents shipped under samples/agents/ (bare YAML + .praimate-agent
// packs). The first directory that exists is used by setup to seed the
// starter agent set.
func SampleAgentCandidates(execDir string) []string {
	return []string{
		filepath.Join(execDir, "samples", "agents"),
		filepath.Join(execDir, "..", "samples", "agents"),
		filepath.Join(execDir, "..", "..", "samples", "agents"),
		filepath.Join(execDir, "..", "share", "praimate", "samples", "agents"),
	}
}

// FirstExistingDir returns the first path in candidates that is an
// existing directory, or "" if none exist.
func FirstExistingDir(candidates []string) string {
	for _, d := range candidates {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return ""
}
