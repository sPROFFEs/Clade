package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinOnPath writes an executable shell script named `name` into a
// temp dir and prepends it to PATH for the test.
func fakeBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExecAdapter_StdoutReply(t *testing.T) {
	// opencode-style: echo the message back on stdout.
	fakeBinOnPath(t, "opencode", `shift; echo "reply: $*"`)
	a := NewOpenCodeAdapter()
	r, err := a.SingleShot(context.Background(), SingleShotOpts{Cwd: t.TempDir(), Message: "hello"})
	if err != nil {
		t.Fatalf("SingleShot: %v", err)
	}
	if !strings.Contains(r.Text, "hello") {
		t.Fatalf("reply did not include message: %q", r.Text)
	}
}

func TestExecAdapter_SystemPromptPrepended(t *testing.T) {
	fakeBinOnPath(t, "opencode", `shift; echo "$*"`)
	a := NewOpenCodeAdapter()
	r, _ := a.SingleShot(context.Background(), SingleShotOpts{
		Cwd: t.TempDir(), Message: "do it", SystemPrompt: "BE-TERSE",
	})
	if !strings.Contains(r.Text, "BE-TERSE") {
		t.Fatalf("system prompt not prepended: %q", r.Text)
	}
}

func TestCodexAdapter_ReadsReplyFile(t *testing.T) {
	// codex writes the final message to the file after --output-last-message.
	fakeBinOnPath(t, "codex", `
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift 2; continue; fi
  shift
done
printf 'FINAL-MSG' > "$out"
echo "noise on stdout"`)
	a := NewCodexAdapter()
	r, err := a.SingleShot(context.Background(), SingleShotOpts{Cwd: t.TempDir(), Message: "x"})
	if err != nil {
		t.Fatalf("SingleShot: %v", err)
	}
	if r.Text != "FINAL-MSG" {
		t.Fatalf("expected reply from file, got %q", r.Text)
	}
}

func TestRegisterAllCLIAdapters_RegistersSeven(t *testing.T) {
	RegisterAllCLIAdapters()
	for _, name := range []string{"claude", "openclaude", "codex", "opencode", "praimate-code", "gemini", "deepseek"} {
		if _, err := GetCLIAdapter(name); err != nil {
			t.Errorf("adapter %q not registered: %v", name, err)
		}
	}
}
