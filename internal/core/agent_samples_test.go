package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// SeedSampleAgents imports bare YAML + packs from a directory, and
// skips agents whose id already exists so a second run is a no-op.
func TestSeedSampleAgents_ImportAndSkipExisting(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, err := New(Options{Store: openTempStore(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sampleYAML := func(id, name string) []byte {
		return []byte("schema: praimate.agent/v1\nid: " + id +
			"\nname: " + name + "\ninstructions: do the thing\nsupports: [claude]\n")
	}

	dir := t.TempDir()
	// One bare-YAML sample…
	if err := os.WriteFile(filepath.Join(dir, "code-review.yaml"),
		sampleYAML("code-review", "Code Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	// …and one pack, exported from an agent we then remove so the pack
	// is the only source.
	if _, err := c.ImportAgentYAML(ctx, sampleYAML("rg-sample", "RG Sample"), ""); err != nil {
		t.Fatalf("seed pack source: %v", err)
	}
	pack := filepath.Join(dir, "rg-sample"+AgentPackExt)
	if err := c.ExportAgentPack(ctx, "rg-sample", pack); err != nil {
		t.Fatalf("ExportAgentPack: %v", err)
	}
	if err := c.DeleteAgent(ctx, "rg-sample"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	imported, err := c.SeedSampleAgents(ctx, dir)
	if err != nil {
		t.Fatalf("SeedSampleAgents: %v", err)
	}
	if len(imported) != 2 {
		t.Fatalf("first seed imported %d agents, want 2 (%v)", len(imported), imported)
	}
	if _, err := c.GetAgent(ctx, "code-review"); err != nil {
		t.Errorf("code-review not imported: %v", err)
	}
	if _, err := c.GetAgent(ctx, "rg-sample"); err != nil {
		t.Errorf("rg-sample pack not imported: %v", err)
	}

	// Second run must skip both (already present) — idempotent.
	again, err := c.SeedSampleAgents(ctx, dir)
	if err != nil {
		t.Fatalf("second SeedSampleAgents: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second seed imported %d, want 0 (skip-existing) %v", len(again), again)
	}
}

// Every sample agent that ships in the repo (samples/agents/) must
// parse, validate, and seed cleanly — a malformed starter agent would
// break first-run setup for fresh installs.
func TestSeedSampleAgents_ShippedSamplesValid(t *testing.T) {
	withTempConfigDir(t)
	dir := filepath.Join("..", "..", "samples", "agents")
	files, err := SampleAgentFiles(dir)
	if err != nil {
		t.Fatalf("SampleAgentFiles: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no samples/agents shipped in this tree")
	}
	ctx := context.Background()
	c, err := New(Options{Store: openTempStore(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	imported, err := c.SeedSampleAgents(ctx, dir)
	if err != nil {
		t.Fatalf("seeding shipped samples failed: %v", err)
	}
	if len(imported) != len(files) {
		t.Errorf("seeded %d of %d shipped samples: %v", len(imported), len(files), imported)
	}
	// The curated set must include the named starters.
	for _, id := range []string{"reverse-ghidra", "code-review", "dev-team", "security-review", "agent-builder"} {
		if _, err := c.GetAgent(ctx, id); err != nil {
			t.Errorf("expected starter agent %q to ship: %v", id, err)
		}
	}
}

// A missing samples dir is not an error — fresh installs without a
// bundled samples/agents/ just import nothing.
func TestSeedSampleAgents_MissingDir(t *testing.T) {
	withTempConfigDir(t)
	ctx := context.Background()
	c, err := New(Options{Store: openTempStore(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.SeedSampleAgents(ctx, filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("SeedSampleAgents on missing dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("imported %d from missing dir, want 0", len(got))
	}
}
