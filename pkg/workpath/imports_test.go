package workpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: write a file with any needed parent dirs.
func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// scaffoldImportFixture builds a tiny templates root in t.TempDir():
//
//	<root>/
//	  _common/foo/
//	    playbook-fragment.md
//	    rules-fragment.md
//	    knowledge/imported.md
//	    tools/imported_tool.sh
//	    agents/imported_agent.md
//	  consumer/
//	    workpath.json     (imports: ["_common/foo"])
//	    mission.md
//	    playbook.md
//	    rules.md
//	    knowledge/native.md
//	    tools/native_tool.sh
//
// Returns the consumer dir.
func scaffoldImportFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	imp := filepath.Join(root, "_common", "foo")
	writeTestFile(t, filepath.Join(imp, "playbook-fragment.md"), "Run imported_tool.sh before judging.\n")
	writeTestFile(t, filepath.Join(imp, "rules-fragment.md"), "Never skip the import stage.\n")
	writeTestFile(t, filepath.Join(imp, "knowledge", "imported.md"), "# imported knowledge\n\nSome notes.\n")
	writeTestFile(t, filepath.Join(imp, "tools", "imported_tool.sh"), "#!/usr/bin/env bash\n# imported tool\necho hi\n")
	writeTestFile(t, filepath.Join(imp, "agents", "imported_agent.md"), "# Imported agent\n\nDoes stuff.\n")

	consumer := filepath.Join(root, "consumer")
	writeTestFile(t, filepath.Join(consumer, "workpath.json"), `{
  "description": "test consumer",
  "imports": ["_common/foo"]
}`)
	writeTestFile(t, filepath.Join(consumer, "mission.md"), "Consume foo.\n")
	writeTestFile(t, filepath.Join(consumer, "playbook.md"), "Step 1: do stuff.\n")
	writeTestFile(t, filepath.Join(consumer, "rules.md"), "Be careful.\n")
	writeTestFile(t, filepath.Join(consumer, "knowledge", "native.md"), "# native knowledge\n\nNotes.\n")
	writeTestFile(t, filepath.Join(consumer, "tools", "native_tool.sh"), "#!/usr/bin/env bash\n# native tool\necho hi\n")
	return consumer
}

func TestLoadDir_MergesImport(t *testing.T) {
	consumer := scaffoldImportFixture(t)
	wp, err := LoadDir(consumer)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if err := Validate(wp); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Tools: native + imported, sorted by name.
	wantTools := []string{"imported_tool", "native_tool"}
	gotTools := []string{}
	for _, tt := range wp.Tools {
		gotTools = append(gotTools, tt.Name)
	}
	if strings.Join(gotTools, ",") != strings.Join(wantTools, ",") {
		t.Errorf("Tools = %v, want %v", gotTools, wantTools)
	}

	// Imported tool carries ImportedFrom; native does not.
	for _, tt := range wp.Tools {
		switch tt.Name {
		case "imported_tool":
			if tt.ImportedFrom == "" {
				t.Errorf("imported_tool ImportedFrom should be set")
			}
			// Resolves to the imported source dir, not the consumer's.
			resolved := wp.ResolveToolScript(tt, tt.Script)
			if !strings.Contains(filepath.ToSlash(resolved), "/_common/foo/tools/") {
				t.Errorf("ResolveToolScript = %q, want it under _common/foo/tools/", resolved)
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("resolved imported script should exist: %v", err)
			}
		case "native_tool":
			if tt.ImportedFrom != "" {
				t.Errorf("native_tool ImportedFrom should be empty, got %q", tt.ImportedFrom)
			}
			resolved := wp.ResolveToolScript(tt, tt.Script)
			if !strings.Contains(filepath.ToSlash(resolved), "/consumer/tools/") {
				t.Errorf("native ResolveToolScript = %q, want it under consumer/tools/", resolved)
			}
		}
	}

	// Agents merged.
	if len(wp.Agents) != 1 || wp.Agents[0].Name != "imported_agent" {
		t.Errorf("Agents = %+v, want [imported_agent]", wp.Agents)
	}
	if wp.Agents[0].ImportedFrom == "" {
		t.Error("imported agent ImportedFrom should be set")
	}

	// Knowledge: native + imported.
	wantKnowledge := []string{"knowledge/imported.md", "knowledge/native.md"}
	gotKnowledge := []string{}
	for _, k := range wp.Knowledge {
		gotKnowledge = append(gotKnowledge, k.RelPath)
	}
	if strings.Join(gotKnowledge, ",") != strings.Join(wantKnowledge, ",") {
		t.Errorf("Knowledge = %v, want %v", gotKnowledge, wantKnowledge)
	}

	// Playbook + rules carry the fragment under headings.
	if !strings.Contains(wp.Playbook, "## Imported capabilities: foo") {
		t.Errorf("Playbook missing imported heading:\n%s", wp.Playbook)
	}
	if !strings.Contains(wp.Playbook, "Run imported_tool.sh") {
		t.Errorf("Playbook missing imported body:\n%s", wp.Playbook)
	}
	if !strings.Contains(wp.Rules, "## Imported rules: foo") {
		t.Errorf("Rules missing imported heading:\n%s", wp.Rules)
	}
}

func TestLoadDir_ConsumerOverridesImportOnCollision(t *testing.T) {
	consumer := scaffoldImportFixture(t)
	// Give the consumer a tool with the same name as the import's tool.
	// The consumer's tool must win.
	writeTestFile(t, filepath.Join(consumer, "tools", "imported_tool.sh"),
		"#!/usr/bin/env bash\n# consumer override of imported_tool\necho overridden\n")
	wp, err := LoadDir(consumer)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	// Find the tool and confirm it resolves to the consumer's script, not the import's.
	for _, tt := range wp.Tools {
		if tt.Name == "imported_tool" {
			if tt.ImportedFrom != "" {
				t.Errorf("collided tool should be the consumer's (ImportedFrom=\"\"), got %q", tt.ImportedFrom)
			}
			resolved := wp.ResolveToolScript(tt, tt.Script)
			if !strings.Contains(filepath.ToSlash(resolved), "/consumer/tools/") {
				t.Errorf("collided tool resolves to %q, want consumer's copy", resolved)
			}
			return
		}
	}
	t.Fatal("imported_tool not found in merged Tools")
}

func TestLoadDir_MissingImportErrors(t *testing.T) {
	root := t.TempDir()
	consumer := filepath.Join(root, "consumer")
	writeTestFile(t, filepath.Join(consumer, "workpath.json"), `{"imports":["_common/does-not-exist"]}`)
	writeTestFile(t, filepath.Join(consumer, "mission.md"), "x.\n")
	_, err := LoadDir(consumer)
	if err == nil {
		t.Fatal("LoadDir should error on missing import; got nil")
	}
	if !strings.Contains(err.Error(), "_common/does-not-exist") {
		t.Errorf("error should name the missing import, got: %v", err)
	}
}

func TestLoadImport_EmptyBundleIsValid(t *testing.T) {
	dir := t.TempDir()
	imp, err := LoadImport(dir)
	if err != nil {
		t.Fatalf("LoadImport of empty dir: %v", err)
	}
	if imp.Name != filepath.Base(dir) {
		t.Errorf("Name = %q, want %q", imp.Name, filepath.Base(dir))
	}
	if imp.PlaybookFragment != "" || imp.RulesFragment != "" ||
		len(imp.Tools) != 0 || len(imp.Agents) != 0 || len(imp.Knowledge) != 0 {
		t.Errorf("empty import should produce empty bundle, got %+v", imp)
	}
}
