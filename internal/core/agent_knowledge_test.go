package core

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempConfigDir redirects os.UserConfigDir for the test so
// knowledge folders land in a scratch dir, not the developer's real
// config.
func withTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch {
	case os.Getenv("XDG_CONFIG_HOME") != "" || os.Getenv("HOME") != "":
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	t.Setenv("AppData", dir) // windows
	return dir
}

func knowledgeAgentYAML(id, mode string) []byte {
	return []byte(`schema: praimate.agent/v1
id: ` + id + `
name: K Agent
instructions: answer from your knowledge
supports: [claude]
knowledge: ` + mode + `
`)
}

// The pack round-trip: export yaml+knowledge as .praimate-agent, wipe,
// import on the "other machine", and the docs + mode arrive intact.
func TestAgentPack_ExportImportRoundTrip(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, _ := New(Options{Store: openTempStore(t)})

	if _, err := c.ImportAgentYAML(ctx, knowledgeAgentYAML("k-agent", "raw"), ""); err != nil {
		t.Fatalf("import yaml: %v", err)
	}
	if _, err := AddAgentKnowledgeFiles("k-agent", []string{writeTempDoc(t, "guide.md", "# Style guide\nrules here")}); err != nil {
		t.Fatalf("add knowledge: %v", err)
	}

	pack := filepath.Join(t.TempDir(), "k-agent"+AgentPackExt)
	if err := c.ExportAgentPack(ctx, "k-agent", pack); err != nil {
		t.Fatalf("export pack: %v", err)
	}

	// "Other machine": fresh DB, wiped knowledge dir.
	c2, _ := New(Options{Store: openTempStore(t)})
	dir, _ := AgentKnowledgeDir("k-agent")
	_ = os.RemoveAll(dir)

	agent, err := c2.ImportAgentPack(ctx, pack)
	if err != nil {
		t.Fatalf("import pack: %v", err)
	}
	if agent.Knowledge != "raw" {
		t.Errorf("knowledge mode = %q", agent.Knowledge)
	}
	files, err := ListAgentKnowledge("k-agent")
	if err != nil || len(files) != 1 || files[0] != "guide.md" {
		t.Fatalf("knowledge files after import = %v (err %v)", files, err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "guide.md"))
	if !strings.Contains(string(body), "Style guide") {
		t.Errorf("knowledge content lost: %q", body)
	}
}

func TestAgentPack_RequirementsScriptRoundTrip(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, _ := New(Options{Store: openTempStore(t)})

	const yamlBody = `schema: praimate.agent/v1
id: requirements-agent
name: Requirements Agent
instructions: prepare the environment
supports: [claude]
requirements:
  os: linux
  script: setup.sh
  instructions: Install Docker before continuing.
`
	if _, err := c.ImportAgentYAML(ctx, []byte(yamlBody), ""); err != nil {
		t.Fatalf("import yaml: %v", err)
	}
	if err := WriteAgentRequirementsScript("requirements-agent", "setup.sh", []byte("#!/bin/sh\necho ready\n")); err != nil {
		t.Fatalf("write requirements script: %v", err)
	}

	pack := filepath.Join(t.TempDir(), "requirements-agent"+AgentPackExt)
	if err := c.ExportAgentPack(ctx, "requirements-agent", pack); err != nil {
		t.Fatalf("export pack: %v", err)
	}

	c2, _ := New(Options{Store: openTempStore(t)})
	agent, err := c2.ImportAgentPack(ctx, pack)
	if err != nil {
		t.Fatalf("import pack: %v", err)
	}
	if agent.Requirements == nil || agent.Requirements.OS != "linux" || agent.Requirements.Script != "setup.sh" {
		t.Fatalf("requirements metadata = %#v", agent.Requirements)
	}
	body, err := ReadAgentRequirementsScript(agent.ID, agent.Requirements.Script)
	if err != nil || !strings.Contains(string(body), "echo ready") {
		t.Fatalf("requirements script = %q (err %v)", body, err)
	}
}

// The system prompt every surface sends must carry the knowledge
// pointer — raw says "read the files", rag says "graphify query" — and
// stays clean for agents without knowledge.
func TestAgentSystemPrompt_KnowledgeNote(t *testing.T) {
	withTempConfigDir(t)
	a := &Agent{ID: "k-note", Name: "K", Instructions: "be helpful", Supports: []string{"claude"}}

	if got := AgentSystemPrompt(a); got != "be helpful" {
		t.Errorf("no-knowledge prompt = %q", got)
	}

	// Mode set but no folder yet → still clean (don't point at nothing).
	a.Knowledge = "raw"
	if got := AgentSystemPrompt(a); got != "be helpful" {
		t.Errorf("missing-folder prompt = %q", got)
	}

	if _, err := AddAgentKnowledgeFiles("k-note", []string{writeTempDoc(t, "a.md", "x")}); err != nil {
		t.Fatalf("add: %v", err)
	}
	raw := AgentSystemPrompt(a)
	if !strings.Contains(raw, "knowledge base") || !strings.Contains(raw, "file tools") {
		t.Errorf("raw prompt missing pointer: %q", raw)
	}
	a.Knowledge = "rag"
	rag := AgentSystemPrompt(a)
	if !strings.Contains(rag, "graphify query") {
		t.Errorf("rag prompt missing graphify guidance: %q", rag)
	}
}

// Zip-slip entries must be rejected, not extracted.
func TestImportAgentPack_RejectsZipSlip(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, _ := New(Options{Store: openTempStore(t)})

	pack := filepath.Join(t.TempDir(), "evil"+AgentPackExt)
	writeZip(t, pack, map[string]string{
		"agent.yaml":                 string(knowledgeAgentYAML("evil", "raw")),
		"knowledge/../../escape.txt": "pwned",
	})
	if _, err := c.ImportAgentPack(ctx, pack); err == nil {
		t.Fatal("zip-slip entry must fail the import")
	}
}

func TestImportAgentPack_InvalidPackPreservesExistingAgent(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, _ := New(Options{Store: openTempStore(t)})

	const original = `schema: praimate.agent/v1
id: existing
name: Existing Agent
instructions: keep me
supports: [claude]
requirements:
  os: linux
  script: setup.sh
`
	if _, err := c.ImportAgentYAML(ctx, []byte(original), ""); err != nil {
		t.Fatalf("import original: %v", err)
	}
	if _, err := AddAgentKnowledgeFiles("existing", []string{writeTempDoc(t, "keep.md", "keep knowledge")}); err != nil {
		t.Fatalf("add original knowledge: %v", err)
	}
	if err := WriteAgentRequirementsScript("existing", "setup.sh", []byte("#!/bin/sh\necho keep\n")); err != nil {
		t.Fatalf("write original requirements: %v", err)
	}

	pack := filepath.Join(t.TempDir(), "invalid"+AgentPackExt)
	writeZip(t, pack, map[string]string{
		"agent.yaml": `schema: praimate.agent/v1
id: existing
name: Replacement Agent
instructions: replace me
supports: [claude]
`,
		"knowledge/new.md":          "new knowledge",
		"requirements/../../bad.sh": "escape",
	})
	if _, err := c.ImportAgentPack(ctx, pack); err == nil {
		t.Fatal("invalid pack must fail")
	}

	agent, err := c.GetAgent(ctx, "existing")
	if err != nil {
		t.Fatalf("get preserved agent: %v", err)
	}
	if agent.Name != "Existing Agent" {
		t.Fatalf("agent was changed after failed import: %q", agent.Name)
	}
	dir, _ := AgentKnowledgeDir("existing")
	body, err := os.ReadFile(filepath.Join(dir, "keep.md"))
	if err != nil || string(body) != "keep knowledge" {
		t.Fatalf("knowledge was changed after failed import: %q (err %v)", body, err)
	}
	body, err = ReadAgentRequirementsScript("existing", "setup.sh")
	if err != nil || !strings.Contains(string(body), "echo keep") {
		t.Fatalf("requirements were changed after failed import: %q (err %v)", body, err)
	}
}

func writeTempDoc(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}
