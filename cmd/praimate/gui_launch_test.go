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

func TestResolveGUIBinary_MissingReturnsEmpty(t *testing.T) {
	// Point exe at an empty dir and ensure PATH can't resolve it either.
	t.Setenv("PATH", t.TempDir())
	got := resolveGUIBinary(filepath.Join(t.TempDir(), "praimate"))
	if got != "" {
		t.Fatalf("expected empty result when gui binary absent, got %q", got)
	}
}
