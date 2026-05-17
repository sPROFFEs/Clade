package targets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdksdk/code-launcher/pkg/workpath"
)

// loadByo loads the shared fixture used by every target test.
func loadByo(t *testing.T) *workpath.Workpath {
	t.Helper()
	wp, err := workpath.LoadDir(filepath.Join("..", "..", "testdata", "byo"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if err := workpath.Validate(wp); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return wp
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestRegistry_HasAllTargets(t *testing.T) {
	want := []string{"claude", "codex", "cursor", "generic", "mika"}
	got := Names()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestClaude_EmitsSkillAndAgent(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	tgt, err := Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Compile(wp, out); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	skillMD := filepath.Join(out, ".claude", "skills", "byo", "SKILL.md")
	body := mustRead(t, skillMD)
	if !strings.HasPrefix(body, "---\n") {
		t.Error("SKILL.md should start with YAML frontmatter")
	}
	if !strings.Contains(body, "name: byo") {
		t.Error("SKILL.md should declare name: byo")
	}
	if !strings.Contains(body, "description: Full-shape fixture") {
		t.Error("SKILL.md should carry description")
	}
	if !strings.Contains(body, "Exercise every loader code path") {
		t.Error("SKILL.md should include mission body verbatim")
	}

	// Scripts copied alongside.
	if _, err := os.Stat(filepath.Join(out, ".claude", "skills", "byo", "scripts", "greet.sh")); err != nil {
		t.Errorf("greet.sh missing: %v", err)
	}

	// Subagent emitted with namespaced name.
	agentMD := filepath.Join(out, ".claude", "agents", "byo__helper.md")
	abody := mustRead(t, agentMD)
	if !strings.Contains(abody, "name: byo__helper") {
		t.Error("agent file should declare namespaced name")
	}
}

func TestMika_EmitsModuleLayout(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	tgt, err := Get("mika")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Compile(wp, out); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	root := filepath.Join(out, "modules", "byo")
	for _, f := range []string{"module.md", "playbook.md", "rules.md", "tools/greet.sh", "tools/count.ps1", "agents/helper.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	module := mustRead(t, filepath.Join(root, "module.md"))
	if !strings.HasPrefix(module, "# byo\n") {
		t.Error("module.md should open with H1 of workpath name")
	}
	if !strings.Contains(module, "> Full-shape fixture") {
		t.Error("module.md should include description as blockquote")
	}
}

func TestCursor_EmitsRule(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	tgt, err := Get("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Compile(wp, out); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rule := mustRead(t, filepath.Join(out, ".cursor", "rules", "byo.mdc"))
	if !strings.Contains(rule, "alwaysApply: false") {
		t.Error("cursor rule should set alwaysApply")
	}
	if !strings.Contains(rule, "## Tools") {
		t.Error("cursor rule should inline tool list")
	}
}

func TestGeneric_EmitsSingleFile(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	tgt, err := Get("generic")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Compile(wp, out); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	body := mustRead(t, filepath.Join(out, "byo.md"))
	if !strings.HasPrefix(body, "# byo\n") {
		t.Error("generic should start with workpath name as H1")
	}
	if !strings.Contains(body, "_Version 2 · MIT_") {
		t.Errorf("generic should include version+license; got: %q", body[:200])
	}
	if _, err := os.Stat(filepath.Join(out, "byo.assets", "tools", "greet.sh")); err != nil {
		t.Errorf("generic assets dir missing greet.sh: %v", err)
	}
}

func TestCodex_EmitsAgentsMD(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	tgt, err := Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tgt.Compile(wp, out); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	body := mustRead(t, filepath.Join(out, "AGENTS.md"))
	if !strings.HasPrefix(body, "# byo\n") {
		t.Error("AGENTS.md should start with workpath name as H1")
	}
	if !strings.Contains(body, "## Tools") {
		t.Error("AGENTS.md should inline tool list")
	}
	if !strings.Contains(body, "AGENTS.assets/tools/") {
		t.Error("AGENTS.md should reference assets dir for tools")
	}
	if _, err := os.Stat(filepath.Join(out, "AGENTS.assets", "tools", "greet.sh")); err != nil {
		t.Errorf("assets dir missing greet.sh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "AGENTS.assets", "agents", "helper.md")); err != nil {
		t.Errorf("assets dir missing helper.md: %v", err)
	}
}

func TestAll_Compiles(t *testing.T) {
	wp := loadByo(t)
	out := t.TempDir()
	for _, tgt := range All() {
		if err := tgt.Compile(wp, out); err != nil {
			t.Errorf("%s: %v", tgt.Name(), err)
		}
	}
}

func TestKebab(t *testing.T) {
	cases := map[string]string{
		"foo_bar":    "foo-bar",
		"FOO BAR":    "foo-bar",
		"already-ok": "already-ok",
	}
	for in, want := range cases {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%q) = %q, want %q", in, got, want)
		}
	}
}
