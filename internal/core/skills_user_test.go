package core

import "testing"

func TestNormaliseGitHubURL(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		subpath   string
		isZip     bool
		ok        bool
	}{
		{"https://github.com/user/repo", "https://github.com/user/repo/archive/refs/heads/main.zip", "", true, true},
		{"https://github.com/user/repo.git", "https://github.com/user/repo/archive/refs/heads/main.zip", "", true, true},
		{"https://github.com/user/repo/tree/dev", "https://github.com/user/repo/archive/refs/heads/dev.zip", "", true, true},
		{"https://github.com/user/repo/tree/main/skills/foo", "https://github.com/user/repo/archive/refs/heads/main.zip", "skills/foo", true, true},
		{"https://github.com/user/repo/blob/main/x.md", "https://raw.githubusercontent.com/user/repo/main/x.md", "", false, true},
		{"https://github.com/user/repo/raw/dev/doc.md", "https://raw.githubusercontent.com/user/repo/dev/doc.md", "", false, true},
		{"https://gist.github.com/user/abc123", "https://gist.githubusercontent.com/user/abc123/raw/", "", false, true},
		{"https://example.com/foo.md", "", "", false, false},
	}
	for _, tc := range cases {
		got, sub, isZip, ok := normaliseGitHubURL(tc.in)
		if got != tc.want || sub != tc.subpath || isZip != tc.isZip || ok != tc.ok {
			t.Errorf("normaliseGitHubURL(%q) = (%q, %q, %v, %v); want (%q, %q, %v, %v)",
				tc.in, got, sub, isZip, ok, tc.want, tc.subpath, tc.isZip, tc.ok)
		}
	}
}

func TestFlipDefaultBranch(t *testing.T) {
	if got, ok := flipDefaultBranch("https://github.com/u/r/archive/refs/heads/main.zip"); !ok || got != "https://github.com/u/r/archive/refs/heads/master.zip" {
		t.Errorf("main→master flip failed: %q %v", got, ok)
	}
	if got, ok := flipDefaultBranch("https://github.com/u/r/archive/refs/heads/master.zip"); !ok || got != "https://github.com/u/r/archive/refs/heads/main.zip" {
		t.Errorf("master→main flip failed: %q %v", got, ok)
	}
	if _, ok := flipDefaultBranch("https://example.com/foo.zip"); ok {
		t.Errorf("flip on unrelated URL should fail")
	}
}
