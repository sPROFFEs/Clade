package workpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDir_HelloMinimal(t *testing.T) {
	wp, err := LoadDir(filepath.Join("..", "..", "testdata", "hello"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if wp.Name != "hello" {
		t.Errorf("Name = %q, want %q", wp.Name, "hello")
	}
	if wp.Version != "1" {
		t.Errorf("Version = %q, want %q", wp.Version, "1")
	}
	if wp.Description == "" {
		t.Error("Description should be auto-derived from mission body")
	}
	if wp.Mission == "" {
		t.Error("Mission should be loaded")
	}
	if len(wp.Tools) != 0 {
		t.Errorf("Tools = %v, want empty", wp.Tools)
	}
	if len(wp.Agents) != 0 {
		t.Errorf("Agents = %v, want empty", wp.Agents)
	}
}

func TestLoadDir_ByoFullShape(t *testing.T) {
	wp, err := LoadDir(filepath.Join("..", "..", "testdata", "byo"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if wp.Description != "Full-shape fixture exercising every wpc loader path" {
		t.Errorf("Description = %q (manifest should win)", wp.Description)
	}
	if wp.Version != "2" {
		t.Errorf("Version = %q, want %q", wp.Version, "2")
	}
	if wp.License != "MIT" {
		t.Errorf("License = %q, want MIT", wp.License)
	}
	if wp.Playbook == "" {
		t.Error("Playbook should be loaded")
	}
	if wp.Rules == "" {
		t.Error("Rules should be loaded")
	}
	if len(wp.Tools) != 2 {
		t.Fatalf("Tools count = %d, want 2", len(wp.Tools))
	}
	// Sorted by name: count (ps1) then greet (sh)
	if wp.Tools[0].Name != "count" || wp.Tools[0].Shell != "pwsh" {
		t.Errorf("Tools[0] = %+v, want count/pwsh", wp.Tools[0])
	}
	if wp.Tools[1].Name != "greet" || wp.Tools[1].Shell != "bash" {
		t.Errorf("Tools[1] = %+v, want greet/bash", wp.Tools[1])
	}
	if wp.Tools[1].Description != "Print a friendly greeting to stdout" {
		t.Errorf("Tools[1].Description = %q (should come from first comment line)", wp.Tools[1].Description)
	}
	if len(wp.Agents) != 1 || wp.Agents[0].Name != "helper" {
		t.Errorf("Agents = %+v, want [helper]", wp.Agents)
	}
	if wp.Agents[0].Description == "" {
		t.Error("Agent description should be auto-derived from first heading")
	}
}

func TestLoadDir_MissingDir(t *testing.T) {
	_, err := LoadDir(filepath.Join("..", "..", "testdata", "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestValidate_RejectsInvalidName(t *testing.T) {
	wp := &Workpath{
		Name:        "Has Spaces",
		Description: "x",
		Mission:     "x",
	}
	if err := Validate(wp); err == nil {
		t.Fatal("expected validation error for invalid name")
	}
}

func TestValidate_RejectsMissingMission(t *testing.T) {
	wp := &Workpath{Name: "x", Description: "x"}
	if err := Validate(wp); err == nil {
		t.Fatal("expected validation error for empty mission")
	}
}

func TestValidate_RejectsUnknownAgentTool(t *testing.T) {
	wp := &Workpath{
		Name:        "x",
		Description: "x",
		Mission:     "x",
		Tools:       []Tool{{Name: "a", Script: "tools/a.sh"}},
		Agents:      []Agent{{Name: "b", Prompt: "agents/b.md", Tools: []string{"nope"}}},
	}
	if err := Validate(wp); err == nil {
		t.Fatal("expected validation error for unknown tool reference")
	}
}

func TestValidate_AcceptsByoFixture(t *testing.T) {
	wp, err := LoadDir(filepath.Join("..", "..", "testdata", "byo"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if err := Validate(wp); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestDeriveDescription(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"# Heading\n\nReal first line.", "Real first line."},
		{"> blockquote\n## sub\n\nactual content here", "actual content here"},
		{"First sentence. Second sentence.", "First sentence."},
		{"# only heading", ""},
	}
	for _, c := range cases {
		got := deriveDescription(c.in)
		if got != c.want {
			t.Errorf("deriveDescription(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// writeWorkpath builds a minimal workpath in a temp dir, writing the
// provided relative-path → body map. Used by the knowledge loader
// tests below.
func writeWorkpath(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadDir_DiscoversKnowledge(t *testing.T) {
	dir := writeWorkpath(t, map[string]string{
		"mission.md":                      "# kn-test\n\ndo a thing.",
		"knowledge/paper.md":              "# Secure Boot Threat Model\n\nA quick summary of attack surface in the secure-boot path. More detail so the summariser has several sentences to work with.",
		"knowledge/notes/cheatsheet.txt":  "first line of cheatsheet\nsecond line",
		"knowledge/datasheets/manual.pdf": "%PDF-fake",
		"knowledge/.hidden":               "should be skipped",
		"knowledge/.dotdir/secret.md":     "should be skipped too",
	})
	wp, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(wp.Knowledge) != 3 {
		t.Fatalf("Knowledge count = %d, want 3, got %#v", len(wp.Knowledge), wp.Knowledge)
	}
	byPath := map[string]KnowledgeFile{}
	for _, k := range wp.Knowledge {
		byPath[k.RelPath] = k
	}
	paper, ok := byPath["knowledge/paper.md"]
	if !ok {
		t.Fatal("knowledge/paper.md not discovered")
	}
	if !paper.IsText {
		t.Errorf("paper.md should be IsText=true")
	}
	if paper.Title != "Secure Boot Threat Model" {
		t.Errorf("paper.Title = %q, want H1 from body", paper.Title)
	}
	if !strings.Contains(paper.Summary, "quick summary") {
		t.Errorf("paper.Summary missing expected text: %q", paper.Summary)
	}
	cheat, ok := byPath["knowledge/notes/cheatsheet.txt"]
	if !ok {
		t.Fatal("nested .txt not discovered")
	}
	if !cheat.IsText {
		t.Errorf(".txt should be IsText=true")
	}
	pdf, ok := byPath["knowledge/datasheets/manual.pdf"]
	if !ok {
		t.Fatal("pdf not discovered")
	}
	if pdf.IsText {
		t.Errorf("pdf should be IsText=false")
	}
	if pdf.Summary != "" {
		t.Errorf("binary should have empty Summary, got %q", pdf.Summary)
	}
}

func TestLoadDir_NoKnowledgeDirIsFine(t *testing.T) {
	dir := writeWorkpath(t, map[string]string{
		"mission.md": "do a thing",
	})
	wp, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(wp.Knowledge) != 0 {
		t.Errorf("Knowledge should be empty, got %v", wp.Knowledge)
	}
}
