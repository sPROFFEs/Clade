package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveGUIBinary_FindsOnPath(t *testing.T) {
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
	// Keep the install-location candidates from matching a real install
	// on the test machine.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())

	got := resolveGUIBinary()
	if got != bin {
		t.Fatalf("resolveGUIBinary = %q, want PATH hit %q", got, bin)
	}
}

func TestResolveGUIBinary_PrefersInstalledApp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("install-candidate layout under test is the Windows one")
	}
	local := t.TempDir()
	installed := filepath.Join(local, "Programs", "PrAImate GUI", "PrAImate GUI.exe")
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("PATH", t.TempDir())

	got := resolveGUIBinary()
	if got != installed {
		t.Fatalf("resolveGUIBinary = %q, want installed app %q", got, installed)
	}
}

func TestResolveGUIBinary_MissingReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("ProgramFiles(x86)", t.TempDir())
	if got := resolveGUIBinary(); got != "" {
		t.Fatalf("expected empty result when gui binary absent, got %q", got)
	}
}
