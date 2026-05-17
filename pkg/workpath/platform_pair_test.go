package workpath

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestDiscoverTools_PlatformPairedScriptsBecomeOneTool reproduces the
// user-reported bug: a workpath with foo.sh + foo.ps1 used to be loaded
// as two separate tools both named "foo", which the validator rejected
// as a duplicate. The loader now merges them into one Tool with two
// script files.
func TestDiscoverTools_PlatformPairedScriptsBecomeOneTool(t *testing.T) {
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	must(t, os.MkdirAll(tools, 0o755))
	// mission.md is required to load the workpath.
	must(t, os.WriteFile(filepath.Join(dir, "mission.md"), []byte("# x\n\nminimal mission.\n"), 0o644))

	// Two paired scripts and one solo.
	must(t, os.WriteFile(filepath.Join(tools, "inventory.sh"),
		[]byte("#!/usr/bin/env bash\n# enumerate inventory (bash)\necho hi\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(tools, "inventory.ps1"),
		[]byte("# enumerate inventory (powershell)\nWrite-Host hi\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(tools, "solo.sh"),
		[]byte("#!/bin/sh\n# solo bash-only tool\necho hi\n"), 0o644))

	wp, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if err := Validate(wp); err != nil {
		t.Fatalf("Validate must accept platform-paired scripts: %v", err)
	}

	if len(wp.Tools) != 2 {
		t.Fatalf("got %d tools, want 2 (inventory + solo)", len(wp.Tools))
	}

	// Find the inventory tool — must have both .sh and .ps1 in its Scripts.
	var inventory *Tool
	for i := range wp.Tools {
		if wp.Tools[i].Name == "inventory" {
			inventory = &wp.Tools[i]
		}
	}
	if inventory == nil {
		t.Fatal("inventory tool missing from catalog")
	}
	scripts := append([]string(nil), inventory.Scripts...)
	sort.Strings(scripts)
	want := []string{"tools/inventory.ps1", "tools/inventory.sh"}
	if !equalStringSlices(scripts, want) {
		t.Errorf("inventory.Scripts = %v, want %v", scripts, want)
	}
	// Primary should be the .sh (preferred over .ps1).
	if inventory.Script != "tools/inventory.sh" {
		t.Errorf("inventory.Script = %q, want tools/inventory.sh as primary", inventory.Script)
	}
	if inventory.Shell != "bash" {
		t.Errorf("inventory.Shell = %q, want bash (.sh primary)", inventory.Shell)
	}
	// The first non-empty description (from the .sh file in this case) wins.
	if inventory.Description == "" {
		t.Error("inventory.Description should come from one of the script files")
	}

	// AllScripts() falls back to [Script] for solo tools.
	var solo *Tool
	for i := range wp.Tools {
		if wp.Tools[i].Name == "solo" {
			solo = &wp.Tools[i]
		}
	}
	if got := solo.AllScripts(); len(got) != 1 || got[0] != "tools/solo.sh" {
		t.Errorf("solo.AllScripts() = %v, want [tools/solo.sh]", got)
	}
}

func equalStringSlices(a, b []string) bool {
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
