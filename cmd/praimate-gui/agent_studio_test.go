package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// setUserConfigDir points os.UserConfigDir at dir on every OS: Windows
// reads %AppData% and ignores XDG_CONFIG_HOME, Unix does the reverse.
func setUserConfigDir(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
		return
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestAgentYAMLDraftUsesHelperWorkspace(t *testing.T) {
	workspace := t.TempDir()
	app := &App{}
	const body = "schema: praimate.agent/v1\nid: demo\n"

	if err := app.WriteAgentYAMLDraftToDisk("demo", workspace, body); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "agent.yaml")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("agent.yaml = %q, want %q", got, body)
	}
}

func TestAgentYAMLDefaultsToAgentDataDirectoryNotProcessCWD(t *testing.T) {
	configDir := t.TempDir()
	launchDir := t.TempDir()
	setUserConfigDir(t, configDir)

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(launchDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCWD) })

	path, err := agentYAMLWorkspacePath("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "praimate", "agents", "demo", "agent.yaml")
	if path != want {
		t.Fatalf("default agent.yaml path = %q, want %q", path, want)
	}
	if filepath.Dir(path) == launchDir {
		t.Fatalf("agent.yaml incorrectly resolved from process cwd %q", launchDir)
	}
}

func TestAgentYAMLExplicitWorkspaceOverridesAgentDataDirectory(t *testing.T) {
	setUserConfigDir(t, t.TempDir())
	workspace := t.TempDir()
	path, err := agentYAMLWorkspacePath("demo", workspace)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workspace, "agent.yaml")
	if path != want {
		t.Fatalf("explicit agent.yaml path = %q, want %q", path, want)
	}
}

func TestAgentYAMLWorkspacePathRequiresAgentID(t *testing.T) {
	if _, err := agentYAMLWorkspacePath("", t.TempDir()); err == nil {
		t.Fatal("expected an empty agent id to be rejected")
	}
}
