package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveGUIBinary_PrefersSibling(t *testing.T) {
	dir := t.TempDir()
	name := "praimate-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sibling := filepath.Join(dir, name)
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExe := filepath.Join(dir, "praimate")

	got := resolveGUIBinary(fakeExe)
	if got != sibling {
		t.Fatalf("resolveGUIBinary = %q, want sibling %q", got, sibling)
	}
}

func TestResolveGUIBinary_FallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	name := "praimate-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	// exe lives in a different, empty dir — no sibling there.
	got := resolveGUIBinary(filepath.Join(t.TempDir(), "praimate"))
	if got != bin {
		t.Fatalf("resolveGUIBinary = %q, want PATH hit %q", got, bin)
	}
}

func TestResolveGUIBinary_MissingReturnsEmpty(t *testing.T) {
	// Point exe at an empty dir and ensure PATH can't resolve it either.
	t.Setenv("PATH", t.TempDir())
	got := resolveGUIBinary(filepath.Join(t.TempDir(), "praimate"))
	if got != "" {
		t.Fatalf("expected empty result when gui binary absent, got %q", got)
	}
}
