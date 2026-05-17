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
	// Ollama, when populated, makes the launcher inject per-launch env
	// vars for Claude so it routes to the local Ollama endpoint instead
	// of Anthropic. Stored here so the choice follows the workspace.
	Ollama OllamaSettings `json:"ollama,omitempty"`
}

// OllamaSettings duplicates internal/ollama.Settings to avoid an import
// cycle (ollama depends on this package's notion of a workspace via
// callers in cmd/code-launcher; keeping these types separate keeps the
// dependency graph one-way).
type OllamaSettings struct {
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	WireAPI  string `json:"wireApi,omitempty"`
}

// SaveWorkspaceSettings persists ws.Settings to <ws.Root>/workspace.json.
// Atomic (tmp + rename).
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

type workpathManifest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

var workspaceNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

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
// git history if the user puts it under version control.
func EnsureSandbox(ws Workspace) error {
	if err := os.MkdirAll(ws.SandboxDir, 0o755); err != nil {
		return err
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
		dst := filepath.Join(workspacesRoot, e.Name(), "workpath")
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
		filepath.Join(execDir, "..", "share", "code-launcher", "samples", "workpaths"),
	}
}
