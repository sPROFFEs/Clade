package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRAGContextHasNoDeadlineAndCancelsWithApp(t *testing.T) {
	appCtx, stopApp := context.WithCancel(context.Background())
	defer stopApp()

	ragCtx, stopRAG := newRAGContext(appCtx)
	defer stopRAG()
	if _, ok := ragCtx.Deadline(); ok {
		t.Fatal("RAG context must not impose a total execution deadline")
	}

	stopApp()
	select {
	case <-ragCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("RAG context was not cancelled when the application stopped")
	}
}

func TestAgentRAGCanBeCancelled(t *testing.T) {
	a := NewApp()
	a.ctx = context.Background()

	ragCtx, done, err := a.beginRAG("agent-1")
	if err != nil {
		t.Fatalf("beginRAG: %v", err)
	}
	defer done()

	a.CancelAgentRAG("agent-1")
	select {
	case <-ragCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("CancelAgentRAG did not cancel the active extraction")
	}
}

func TestRequirementsCommandUsesBashForUnixShellScripts(t *testing.T) {
	name, args := requirementsCommand("linux", "/tmp/setup.sh")

	if name != "bash" {
		t.Fatalf("command = %q, want bash", name)
	}
	if want := []string{"/tmp/setup.sh"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestPrepareSudoAskpassUsesConfiguredHelper(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUDO_ASKPASS", helper)

	got, cleanup, err := prepareSudoAskpass("linux")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got != helper {
		t.Fatalf("helper = %q, want %q", got, helper)
	}
}

func TestPrepareSudoPopupWrapperUsesPolicyKit(t *testing.T) {
	pkexec := filepath.Join(t.TempDir(), "pkexec")
	if err := os.WriteFile(pkexec, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	dir, cleanup, err := prepareSudoPopupWrapper(pkexec, "/usr/bin/sudo")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out, err := exec.Command(filepath.Join(dir, "sudo"), "apt-get", "update").CombinedOutput()
	if err != nil {
		t.Fatalf("run wrapper: %v: %s", err, out)
	}
	if got, want := string(out), "/usr/bin/sudo\napt-get\nupdate\n"; got != want {
		t.Fatalf("wrapper args = %q, want %q", got, want)
	}
}

func TestAgentRequirementsCanBeCancelled(t *testing.T) {
	a := NewApp()
	a.ctx = context.Background()

	ctx, done, err := a.beginRequirements("agent-1")
	if err != nil {
		t.Fatalf("beginRequirements: %v", err)
	}
	defer done()
	if _, _, err := a.beginRequirements("agent-1"); err == nil {
		t.Fatal("second requirements run for the same agent must be rejected")
	}

	a.CancelAgentRequirements("agent-1")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("CancelAgentRequirements did not cancel the active script")
	}
}
