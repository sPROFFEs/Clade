package launcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// samplesDir returns the absolute path to the repo's bundled samples,
// regardless of where `go test` is invoked from.
func samplesDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// internal/launcher → up two = repo root.
	return filepath.Join(cwd, "..", "..", "samples", "workpaths")
}

func TestSeedSamples_CopiesEveryBundledWorkpath(t *testing.T) {
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s: %v", src, err)
	}

	root := t.TempDir()
	seeded, err := SeedSamples(root, []string{src})
	if err != nil {
		t.Fatalf("SeedSamples: %v", err)
	}
	sort.Strings(seeded)
	if want := []string{"code-review", "reversing"}; !equalStrings(seeded, want) {
		t.Errorf("seeded = %v, want %v", seeded, want)
	}

	// Both should now appear via ListWorkspaces, each with a workpath dir.
	got, err := ListWorkspaces(root)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListWorkspaces returned %d, want 2", len(got))
	}
	for _, ws := range got {
		if _, err := os.Stat(ws.WorkpathDir); err != nil {
			t.Errorf("workpath dir missing for %s: %v", ws.Name, err)
		}
		if ws.Description == "" {
			t.Errorf("workspace %s has empty description", ws.Name)
		}
	}
}

func TestSeedSamples_SkipsExisting(t *testing.T) {
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	// Mutate one of them to detect clobbering.
	mark := filepath.Join(root, "reversing", "workpath", "mission.md")
	if err := os.WriteFile(mark, []byte("MUTATED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedSamples(root, []string{src})
	if err != nil {
		t.Fatalf("seed2: %v", err)
	}
	if len(seeded) != 0 {
		t.Errorf("second seed should be a no-op, got %v", seeded)
	}
	raw, _ := os.ReadFile(mark)
	if string(raw) != "MUTATED\n" {
		t.Error("second seed clobbered an existing workpath")
	}
}

func TestListWorkspaces_IgnoresHiddenAndNonWorkpathDirs(t *testing.T) {
	root := t.TempDir()
	// real one
	must(t, os.MkdirAll(filepath.Join(root, "good", "workpath"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "good", "workpath", "mission.md"), []byte("# good\n"), 0o644))
	// hidden
	must(t, os.MkdirAll(filepath.Join(root, ".hidden", "workpath"), 0o755))
	// non-workpath dir
	must(t, os.MkdirAll(filepath.Join(root, "stray"), 0o755))

	got, err := ListWorkspaces(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("got %+v, want exactly [good]", got)
	}
}

func TestCreateWorkspace_ScaffoldsValidWorkpath(t *testing.T) {
	root := t.TempDir()
	ws, err := CreateWorkspace(root, "my-new-thing", "describes what this does")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	for _, f := range []string{"workpath.json", "mission.md", "playbook.md", "rules.md"} {
		if _, err := os.Stat(filepath.Join(ws.WorkpathDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if _, err := os.Stat(ws.SandboxDir); err != nil {
		t.Errorf("sandbox not created: %v", err)
	}
	// Sandbox should be gitignored by default.
	gi, err := os.ReadFile(filepath.Join(ws.SandboxDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read sandbox .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), "*") {
		t.Errorf("sandbox .gitignore should ignore everything; got %q", gi)
	}
}

func TestCreateWorkspace_RejectsBadName(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "Bad-Caps", "_leading-underscore", "spaces here", "-leading"} {
		if _, err := CreateWorkspace(root, bad, "x"); err == nil {
			t.Errorf("CreateWorkspace(%q) should have failed", bad)
		}
	}
}

func TestCreateWorkspace_RefusesDuplicate(t *testing.T) {
	root := t.TempDir()
	if _, err := CreateWorkspace(root, "dup", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWorkspace(root, "dup", "second"); err == nil {
		t.Error("second create should have failed")
	}
}

func TestEnsureSandbox_Idempotent(t *testing.T) {
	root := t.TempDir()
	ws, err := CreateWorkspace(root, "ws1", "x")
	if err != nil {
		t.Fatal(err)
	}
	// Write a marker file inside the sandbox.
	mark := filepath.Join(ws.SandboxDir, "mark")
	if err := os.WriteFile(mark, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-ensure — must not delete the marker.
	if err := EnsureSandbox(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("EnsureSandbox wiped existing files: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
