package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBinOnPath writes an executable shell script named `name` into a
// temp dir and prepends it to PATH for the test. Unix-only — callers
// must skip on Windows.
func fakeBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-script fake binaries don't run on Windows")
	}
}

func TestExecAdapter_StdoutReply(t *testing.T) {
	skipOnWindows(t)
	// opencode-style: message arrives on stdin now, echo it back.
	fakeBinOnPath(t, "opencode", `echo "reply: $(cat -)"`)
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
	skipOnWindows(t)
	fakeBinOnPath(t, "opencode", `cat -`)
	a := NewOpenCodeAdapter()
	r, _ := a.SingleShot(context.Background(), SingleShotOpts{
		Cwd: t.TempDir(), Message: "do it", SystemPrompt: "BE-TERSE",
	})
	if !strings.Contains(r.Text, "BE-TERSE") {
		t.Fatalf("system prompt not prepended: %q", r.Text)
	}
}

// Pins the truncation fix: a multi-line system prompt + message must
// reach the CLI intact. Before the stdin switch, the message rode argv,
// which Windows .CMD shims truncate at the first newline (the "your
// prompt cuts off after ..." bug).
func TestExecAdapter_MultilineMessageSurvives(t *testing.T) {
	skipOnWindows(t)
	fakeBinOnPath(t, "opencode", `cat -`)
	a := NewOpenCodeAdapter()
	system := "You are the Agent Builder. Your job is to help the user\ncreate a new agent and hand them a valid YAML file."
	r, err := a.SingleShot(context.Background(), SingleShotOpts{
		Cwd: t.TempDir(), Message: "line1\nline2\nline3", SystemPrompt: system,
	})
	if err != nil {
		t.Fatalf("SingleShot: %v", err)
	}
	for _, want := range []string{"valid YAML file", "line3"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("multi-line content truncated, missing %q: %q", want, r.Text)
		}
	}
}

func TestCodexAdapter_ReadsReplyFile(t *testing.T) {
	skipOnWindows(t)
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

// Pure unit tests for the argv shapes — these run on every OS and pin
// that no adapter embeds the message in argv when stdinMsg is set.
func TestExecAdapter_StdinAdaptersKeepMessageOutOfArgv(t *testing.T) {
	const msg = "multi\nline\nmessage"
	cases := []*execAdapter{
		NewCodexAdapter(),
		NewOpenCodeAdapter(),
		NewPraimateCodeAdapter(),
		NewGeminiAdapter(),
	}
	for _, a := range cases {
		if !a.stdinMsg {
			t.Errorf("%s: expected stdinMsg=true", a.name)
		}
		args, _ := a.build(msg, t.TempDir())
		for _, arg := range args {
			if strings.Contains(arg, "multi") {
				t.Errorf("%s: message leaked into argv: %q", a.name, args)
			}
		}
	}
}

func TestCodexAdapter_UsesStdinSentinel(t *testing.T) {
	args, _ := NewCodexAdapter().build("ignored", t.TempDir())
	if args[len(args)-1] != "-" {
		t.Fatalf("codex argv must end with '-' (read prompt from stdin), got %v", args)
	}
}

func TestIsBatchShim(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\Users\u\AppData\Local\pnpm\claude.CMD`: true,
		`C:\nodejs\gemini.cmd`:                     true,
		`C:\scripts\run.bat`:                       true,
		`C:\Program Files\claude\claude.exe`:       false,
		`/usr/local/bin/claude`:                    false,
	} {
		if got := isBatchShim(path); got != want {
			t.Errorf("isBatchShim(%q) = %v, want %v", path, got, want)
		}
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
