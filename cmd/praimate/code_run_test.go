package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveCodeBinary_PrefersSibling(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, codeBinaryName())
	if err := os.WriteFile(sibling, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveCodeBinary(filepath.Join(dir, "praimate"), t.TempDir())
	if got != sibling {
		t.Fatalf("resolveCodeBinary = %q, want %q", got, sibling)
	}
}

func TestResolveCodeBinary_FallsBackToConfigDir(t *testing.T) {
	emptyExeDir := t.TempDir()
	cfg := t.TempDir()
	binDir := filepath.Join(cfg, "praimate", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(binDir, codeBinaryName())
	if err := os.WriteFile(managed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ensure PATH can't accidentally resolve it.
	t.Setenv("PATH", t.TempDir())
	got := resolveCodeBinary(filepath.Join(emptyExeDir, "praimate"), cfg)
	if got != managed {
		t.Fatalf("resolveCodeBinary = %q, want managed %q", got, managed)
	}
}

func TestResolveCodeBinary_MissingReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := resolveCodeBinary(filepath.Join(t.TempDir(), "praimate"), t.TempDir())
	if got != "" {
		t.Fatalf("expected empty when absent, got %q", got)
	}
}

func TestCodeBinaryName_OSSuffix(t *testing.T) {
	name := codeBinaryName()
	if runtime.GOOS == "windows" && name != "praimate-code.exe" {
		t.Fatalf("windows name = %q", name)
	}
	if runtime.GOOS != "windows" && name != "praimate-code" {
		t.Fatalf("unix name = %q", name)
	}
}
