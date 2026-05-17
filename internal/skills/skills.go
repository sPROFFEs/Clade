// Package skills fetches "online skills" — Claude-Code-style skill bundles
// hosted in a git repo — into a target directory the host CLI can load.
//
// We only support git URLs (HTTPS, SSH, or file://) because git is on
// nearly every dev box and Claude Code's caveman, sequential-thinking,
// and other community skills all ship as git repos. Zip support is a
// future extension.
package skills

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// FetchResult records what happened to one skill URL.
type FetchResult struct {
	URL  string
	Path string // local dir the skill landed in (empty on failure)
	Err  error
}

// FetchAll clones every URL into targetRoot/<derived-name>/. Skips URLs
// whose target dir already exists (idempotent across launches). Returns
// one result per input; never errors as a whole.
func FetchAll(ctx context.Context, urls []string, targetRoot string) []FetchResult {
	results := make([]FetchResult, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		results = append(results, fetchOne(ctx, u, targetRoot))
	}
	return results
}

func fetchOne(ctx context.Context, raw, targetRoot string) FetchResult {
	name, err := DeriveName(raw)
	if err != nil {
		return FetchResult{URL: raw, Err: err}
	}
	dst := filepath.Join(targetRoot, name)
	if _, err := os.Stat(dst); err == nil {
		// Already cloned — treat as success without re-cloning.
		return FetchResult{URL: raw, Path: dst}
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return FetchResult{URL: raw, Err: err}
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", raw, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Clean up partial clone if any so a retry doesn't get stuck on
		// "already exists".
		_ = os.RemoveAll(dst)
		return FetchResult{URL: raw, Err: fmt.Errorf("git clone failed: %v: %s", err, strings.TrimSpace(string(out)))}
	}
	return FetchResult{URL: raw, Path: dst}
}

// DeriveName turns a git URL into a sensible directory name. Mostly the
// repo's last path segment with `.git` stripped.
//
// Examples:
//   https://github.com/juliusbrussee/caveman.git → caveman
//   git@github.com:org/Some-Repo.git            → some-repo
//   file:///tmp/myrepo                          → myrepo
//   C:\Users\me\repos\myrepo                    → myrepo
func DeriveName(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	// Handle scp-style git@host:path
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		raw = parts[1]
	} else if hasURLScheme(raw) {
		if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
			raw = u.Path
		}
	}
	// Normalize separator so filepath.Base works on both flavors.
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.TrimRight(raw, "/")
	base := raw
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		base = raw[i+1:]
	}
	base = strings.TrimSuffix(base, ".git")
	base = strings.ToLower(base)
	base = nonSlug.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" || base == "." {
		return "", fmt.Errorf("cannot derive name from %q", raw)
	}
	return base, nil
}

// hasURLScheme returns true only for the schemes we actually expect for
// git remotes, so a Windows path like "C:\foo" isn't misread as a URL
// (Go's url.Parse will happily treat "C" as a scheme).
func hasURLScheme(s string) bool {
	for _, p := range []string{"http://", "https://", "ssh://", "git://", "file://"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			return true
		}
	}
	return false
}

var nonSlug = regexp.MustCompile(`[^a-z0-9_-]+`)
