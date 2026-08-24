package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestListCLIBackendsRefreshesUserPATHBeforeDetection(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		t.Setenv("PATHEXT", ".BAT;.CMD;.EXE")
	} else {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	}
	t.Setenv("PATH", "")

	binDir := filepath.Join(home, ".bun", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "codex"
	body := "#!/bin/sh\necho codex 9.9.9\n"
	if runtime.GOOS == "windows" {
		name = "codex.bat"
		body = "@echo off\r\necho codex 9.9.9\r\n"
	}
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.ctx = context.Background()
	backends, err := app.ListCLIBackends()
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range backends {
		if backend.ID == "codex" {
			if !backend.Installed {
				t.Fatalf("Codex in a newly-created user bin dir was not detected: %+v", backend)
			}
			return
		}
	}
	t.Fatal("Codex missing from CLI catalogue")
}
