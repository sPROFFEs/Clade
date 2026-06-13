package core

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sample agents are a curated starter set shipped on disk under
// samples/agents/ (next to the binary in a release), NOT embedded
// builtins. They are offered as an opt-in import during first-run setup
// so a fresh install can be tried out immediately, while leaving users
// who don't want them with a clean slate.
//
// Unlike builtins (SeedBuiltins), sample agents are imported once and
// then owned by the user — re-running setup or editing them never
// re-clobbers their copy, because we skip any id that already exists.

// SampleAgentFiles returns the importable sample-agent files (bare YAML
// and .praimate-agent packs) found in dir, sorted by name. Returns an
// empty slice (no error) when dir is absent so callers can treat
// "no samples shipped" as a normal case.
func SampleAgentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yaml", ".yml", AgentPackExt, ".zip":
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// SeedSampleAgents imports every sample agent from dir, skipping any
// whose id already exists in the store (so it is safe to call on every
// setup and never clobbers a user's edited copy). Returns the names of
// the agents actually imported.
func (c *Core) SeedSampleAgents(ctx context.Context, dir string) ([]string, error) {
	if c.store == nil {
		return nil, nil
	}
	files, err := SampleAgentFiles(dir)
	if err != nil {
		return nil, err
	}
	existing, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(existing))
	for _, a := range existing {
		have[a.ID] = true
	}

	var imported []string
	for _, f := range files {
		// Peek the id cheaply so we can skip already-present agents
		// without mutating their knowledge folder.
		id, err := peekAgentID(f)
		if err == nil && id != "" && have[id] {
			continue
		}
		a, err := c.ImportAgentAuto(ctx, f)
		if err != nil {
			return imported, fmt.Errorf("import sample %s: %w", filepath.Base(f), err)
		}
		have[a.ID] = true
		imported = append(imported, a.Name)
	}
	return imported, nil
}

// peekAgentID reads just the agent id from a sample file without
// importing it. Handles bare YAML and .praimate-agent / .zip packs
// (which carry agent.yaml at the root).
func peekAgentID(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case AgentPackExt, ".zip":
		zr, err := zip.OpenReader(path)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if zf.Name != "agent.yaml" {
				continue
			}
			r, err := zf.Open()
			if err != nil {
				return "", err
			}
			body, err := io.ReadAll(io.LimitReader(r, 1<<20))
			_ = r.Close()
			if err != nil {
				return "", err
			}
			a, err := ParseAgentYAML(strings.NewReader(string(body)))
			if err != nil {
				return "", err
			}
			return a.ID, nil
		}
		return "", fmt.Errorf("%s has no agent.yaml", filepath.Base(path))
	default:
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		a, err := ParseAgentYAML(f)
		if err != nil {
			return "", err
		}
		return a.ID, nil
	}
}
