package core

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// BuiltinAgents returns every agent shipped in the binary, parsed and
// validated. Used both for the GUI's "create from built-in" affordance
// and for SeedBuiltins() which installs them into the user's DB on
// first launch.
func BuiltinAgents() ([]*Agent, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, fmt.Errorf("read builtin agents: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	out := make([]*Agent, 0, len(names))
	for _, name := range names {
		body, err := fs.ReadFile(builtinFS, path.Join("builtin", name))
		if err != nil {
			return nil, fmt.Errorf("read builtin %s: %w", name, err)
		}
		a, err := ParseAgentYAML(strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("parse builtin %s: %w", name, err)
		}
		a.SourcePath = "builtin:" + name
		out = append(out, a)
	}
	return out, nil
}

// SeedBuiltins upserts every built-in agent into the user's DB. Safe
// to run on every startup: existing ids are updated in place, so
// editing a built-in YAML and rebuilding picks up the change.
//
// Returns the number of agents seeded.
func (c *Core) SeedBuiltins(ctx context.Context) (int, error) {
	if c.store == nil {
		return 0, nil
	}
	agents, err := BuiltinAgents()
	if err != nil {
		return 0, err
	}
	for _, a := range agents {
		if _, err := c.upsertAgent(ctx, a); err != nil {
			return 0, fmt.Errorf("seed %s: %w", a.ID, err)
		}
	}
	return len(agents), nil
}
