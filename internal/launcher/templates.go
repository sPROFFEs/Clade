package launcher

// Templates are reusable workpath patterns. They live under
// <root>/templates/<name>/workpath/. The launcher never runs an agent
// against a template directly — `New chat` clones the template's
// workpath into a fresh <root>/chats/<id>/ directory and runs the
// agent from there.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TemplatesDir is the conventional location for templates under root.
const TemplatesDir = "templates"

// Template is the read-only pattern from which chats are cloned.
type Template struct {
	Name        string
	Root        string // <workspacesRoot>/templates/<name>
	WorkpathDir string // <Root>/workpath
	Description string
	Settings    WorkspaceSettings
}

// ListTemplates returns every template under <root>/templates/, sorted.
func ListTemplates(root string) ([]Template, error) {
	dir := filepath.Join(root, TemplatesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		t, err := LoadTemplate(root, e.Name())
		if err != nil || t == nil {
			continue
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadTemplate returns nil (no error) when the dir exists but isn't a
// well-formed template (no workpath subdir).
func LoadTemplate(root, name string) (*Template, error) {
	tRoot := filepath.Join(root, TemplatesDir, name)
	wpDir := filepath.Join(tRoot, "workpath")
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
	settings, _ := readSettings(filepath.Join(tRoot, "template.json"))
	desc := manifest.Description
	if desc == "" {
		desc = firstParagraph(filepath.Join(wpDir, "mission.md"))
	}
	return &Template{
		Name:        name,
		Root:        tRoot,
		WorkpathDir: wpDir,
		Description: desc,
		Settings:    settings,
	}, nil
}

// CreateTemplate scaffolds an empty template (mission/playbook/rules
// boilerplate) under <root>/templates/<name>/. Returns an error if the
// name is invalid or the template already exists.
func CreateTemplate(root, name, description string) (Template, error) {
	if !workspaceNameRE.MatchString(name) {
		return Template{}, fmt.Errorf("name must match [a-z0-9][a-z0-9_-]*, got %q", name)
	}
	tRoot := filepath.Join(root, TemplatesDir, name)
	if _, err := os.Stat(tRoot); err == nil {
		return Template{}, fmt.Errorf("template already exists: %s", tRoot)
	}
	wpDir := filepath.Join(tRoot, "workpath")
	if err := os.MkdirAll(filepath.Join(wpDir, "tools"), 0o755); err != nil {
		return Template{}, err
	}
	if err := os.MkdirAll(filepath.Join(wpDir, "agents"), 0o755); err != nil {
		return Template{}, err
	}
	manifestRaw, _ := json.MarshalIndent(struct {
		Description string `json:"description"`
		Version     string `json:"version"`
	}{description, "1"}, "", "  ")
	if err := os.WriteFile(filepath.Join(wpDir, "workpath.json"), append(manifestRaw, '\n'), 0o644); err != nil {
		return Template{}, err
	}
	mission := fmt.Sprintf("# %s\n\n%s\n\nDescribe the mission this workpath exists to accomplish — the desired outcome, the inputs the agent will receive, and the artifacts it should produce.\n", name, description)
	if err := os.WriteFile(filepath.Join(wpDir, "mission.md"), []byte(mission), 0o644); err != nil {
		return Template{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "playbook.md"),
		[]byte("## Stage 1 — Scout\n\n- (describe step)\n\n## Stage 2 — Execute\n\n- (describe step)\n"), 0o644); err != nil {
		return Template{}, err
	}
	if err := os.WriteFile(filepath.Join(wpDir, "rules.md"),
		[]byte("- Never (hard constraint)\n- Always (hard constraint)\n"), 0o644); err != nil {
		return Template{}, err
	}
	return Template{Name: name, Root: tRoot, WorkpathDir: wpDir, Description: description}, nil
}

// DeleteTemplate removes the template's directory tree entirely. Existing
// chats cloned from it are unaffected — they have their own copy of the
// workpath.
func DeleteTemplate(root, name string) error {
	tRoot := filepath.Join(root, TemplatesDir, name)
	if _, err := os.Stat(tRoot); err != nil {
		return err
	}
	return os.RemoveAll(tRoot)
}

// SaveTemplateSettings persists settings to <Template.Root>/template.json.
func SaveTemplateSettings(t Template) error {
	raw, err := json.MarshalIndent(t.Settings, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := filepath.Join(t.Root, "template.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
