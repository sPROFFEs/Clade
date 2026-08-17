package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedProjectBrokerContainsPathsAndRequiresWriteApproval(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	approved := false
	broker, err := newManagedToolBroker(context.Background(), nil, AgentCapabilities{ReadProject: true, ModifyFiles: true}, root, &ApprovalConfig{
		Request: func(_ context.Context, tool string, _ map[string]any) (bool, error) {
			return approved && tool == "project.write", nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	if _, err := broker.ExecuteTool(context.Background(), "project.read", []byte(`{"path":"escape/secret.txt"}`)); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlink escape err = %v", err)
	}
	if _, err := broker.ExecuteTool(context.Background(), "project.write", []byte(`{"path":"new.txt","content":"new"}`)); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("denied write err = %v", err)
	}
	approved = true
	if _, err := broker.ExecuteTool(context.Background(), "project.write", []byte(`{"path":"new.txt","content":"new"}`)); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "new.txt")); err != nil || string(body) != "new" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestManagedCommandBrokerUsesArgvAndApproval(t *testing.T) {
	root := t.TempDir()
	var input map[string]any
	broker, err := newManagedToolBroker(context.Background(), nil, AgentCapabilities{ExecuteCommands: true}, root, &ApprovalConfig{
		Request: func(_ context.Context, _ string, got map[string]any) (bool, error) { input = got; return true, nil },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	out, err := broker.ExecuteTool(context.Background(), "command.run", []byte(`{"command":"go","args":["version"],"timeout_seconds":5}`))
	if err != nil || !strings.Contains(out, "go version") || input["command"] != "go" {
		t.Fatalf("out=%q input=%#v err=%v", out, input, err)
	}
}
