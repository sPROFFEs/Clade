package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDeriveName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/juliusbrussee/caveman.git": "caveman",
		"https://github.com/org/Cool-Tool":             "cool-tool",
		"git@github.com:org/Some-Repo.git":             "some-repo",
		"file:///tmp/myrepo":                           "myrepo",
		"myrepo":                                       "myrepo",
		"myrepo.git":                                   "myrepo",
		"https://example.com/p/under_scored":           "under_scored",
		"https://example.com/p/MixCASE":                "mixcase",
	}
	for in, want := range cases {
		got, err := DeriveName(in)
		if err != nil {
			t.Errorf("DeriveName(%q) err: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("DeriveName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFetchAll_ClonesLocalRepo(t *testing.T) {
	// Skip if git isn't installed (some CI containers lack it).
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}

	// Build a minimal local bare repo to act as the "remote".
	src := filepath.Join(t.TempDir(), "fakeskill")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# fakeskill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.email=t@e", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	target := filepath.Join(t.TempDir(), "skills")
	results := FetchAll(context.Background(), []string{src}, target)
	if len(results) != 1 {
		t.Fatalf("results = %v", results)
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("FetchAll err: %v", r.Err)
	}
	want := filepath.Join(target, "fakeskill", "SKILL.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected %s: %v", want, err)
	}

	// Idempotent: second call shouldn't re-clone.
	mtime1Path := filepath.Join(target, "fakeskill")
	stat1, _ := os.Stat(mtime1Path)
	results = FetchAll(context.Background(), []string{src}, target)
	stat2, _ := os.Stat(mtime1Path)
	if stat1.ModTime() != stat2.ModTime() {
		t.Error("second FetchAll re-cloned (mtime changed)")
	}
	if results[0].Err != nil {
		t.Errorf("second call err: %v", results[0].Err)
	}
}

func TestFetchAll_CapturesPerURLErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH")
	}
	target := t.TempDir()
	// Nonexistent path → git clone fails.
	results := FetchAll(context.Background(), []string{"/this/path/does/not/exist"}, target)
	if len(results) != 1 || results[0].Err == nil {
		t.Errorf("expected error, got %v", results)
	}
}
