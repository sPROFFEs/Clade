package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestTerminalSnapshotRetainsOutputAndOffsets(t *testing.T) {
	tm := newTermManager()
	tm.sessions["term-1"] = &termSession{id: "term-1"}

	first, ok := tm.recordOutput("term-1", []byte("hello "))
	if !ok || first.StartOffset != 0 || first.EndOffset != 6 {
		t.Fatalf("first chunk = %+v, ok=%v", first, ok)
	}
	second, ok := tm.recordOutput("term-1", []byte("world"))
	if !ok || second.StartOffset != 6 || second.EndOffset != 11 {
		t.Fatalf("second chunk = %+v, ok=%v", second, ok)
	}

	snapshot, err := tm.snapshot("term-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(snapshot.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello world" || snapshot.StartOffset != 0 || snapshot.EndOffset != 11 {
		t.Fatalf("snapshot = %+v, raw=%q", snapshot, raw)
	}
}

func TestTerminalSnapshotKeepsBoundedTail(t *testing.T) {
	tm := newTermManager()
	tm.sessions["term-1"] = &termSession{id: "term-1"}
	payload := []byte(strings.Repeat("x", terminalHistoryLimit+32))
	chunk, ok := tm.recordOutput("term-1", payload)
	if !ok {
		t.Fatal("recordOutput returned false")
	}
	snapshot, err := tm.snapshot("term-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StartOffset != 32 || snapshot.EndOffset != int64(len(payload)) {
		t.Fatalf("bounded offsets = %+v (chunk=%+v)", snapshot, chunk)
	}
	raw, _ := base64.StdEncoding.DecodeString(snapshot.Data)
	if len(raw) != terminalHistoryLimit {
		t.Fatalf("snapshot bytes = %d, want %d", len(raw), terminalHistoryLimit)
	}
}

func TestCodeSessionHistoryPersistsAcrossTerminalReplacement(t *testing.T) {
	tm := newTermManager()
	tm.historyDir = t.TempDir()
	tm.sessions["old"] = &termSession{id: "old"}

	_, _ = tm.recordOutput("old", []byte("first process\r\n"))
	if err := tm.bindChat("old", "chat-1"); err != nil {
		t.Fatal(err)
	}
	_, _ = tm.recordOutput("old", []byte("after binding\r\n"))
	// Reattaching the same live terminal must not flush its retained prefix a
	// second time.
	if err := tm.bindChat("old", "chat-1"); err != nil {
		t.Fatal(err)
	}
	tm.close("old")

	tm.sessions["new"] = &termSession{id: "new"}
	_, _ = tm.recordOutput("new", []byte("replacement process\r\n"))
	if err := tm.bindChat("new", "chat-1"); err != nil {
		t.Fatal(err)
	}

	snapshot, err := tm.codeSnapshot("chat-1", "new")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(snapshot.Data)
	if err != nil {
		t.Fatal(err)
	}
	want := "first process\r\nafter binding\r\nreplacement process\r\n"
	if string(raw) != want {
		t.Fatalf("persistent transcript = %q, want %q", raw, want)
	}
	if snapshot.EndOffset != int64(len("replacement process\r\n")) {
		t.Fatalf("live offset = %d", snapshot.EndOffset)
	}

	// The same transcript remains readable with no live process, as happens
	// after restarting the application.
	tm.close("new")
	archived, err := tm.codeSnapshot("chat-1", "")
	if err != nil {
		t.Fatal(err)
	}
	archivedRaw, _ := base64.StdEncoding.DecodeString(archived.Data)
	if string(archivedRaw) != want || archived.EndOffset != 0 {
		t.Fatalf("archived snapshot = %+v, raw=%q", archived, archivedRaw)
	}
}

func TestBindChatRejectsMissingOrConflictingTerminal(t *testing.T) {
	tm := newTermManager()
	tm.historyDir = t.TempDir()
	if err := tm.bindChat("missing", "chat-1"); err == nil {
		t.Fatal("expected missing terminal error")
	}
	tm.sessions["term-1"] = &termSession{id: "term-1"}
	if err := tm.bindChat("term-1", "chat-1"); err != nil {
		t.Fatal(err)
	}
	if err := tm.bindChat("term-1", "chat-2"); err == nil {
		t.Fatal("expected conflicting chat binding error")
	}
}

func TestTerminalEnvironmentPreservesHostAndAppliesOverrides(t *testing.T) {
	got := environmentByKey(terminalEnvironment(
		[]string{"PATH=/usr/bin", "HOME=/home/test", "LANG=es_ES.UTF-8", "TOKEN=old"},
		[]string{"TOKEN=nuevo-á", "OPENAI_BASE_URL=http://localhost:11434/v1"},
	))
	if got["PATH"] != "/usr/bin" || got["HOME"] != "/home/test" {
		t.Fatalf("host environment was not preserved: %#v", got)
	}
	if got["TOKEN"] != "nuevo-á" {
		t.Fatalf("override was not applied as UTF-8: %q", got["TOKEN"])
	}
	if got["LANG"] != "es_ES.UTF-8" {
		t.Fatalf("UTF-8 locale changed unexpectedly: %q", got["LANG"])
	}
	if got["TERM"] != "xterm-256color" || got["COLORTERM"] != "truecolor" {
		t.Fatalf("terminal capabilities missing: %#v", got)
	}
}

func TestTerminalEnvironmentUpgradesNonUTF8Locale(t *testing.T) {
	got := environmentByKey(terminalEnvironment([]string{"PATH=/usr/bin", "LC_ALL=C", "LANG=C"}, nil))
	if got["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("LC_ALL = %q, want C.UTF-8", got["LC_ALL"])
	}

	got = environmentByKey(terminalEnvironment([]string{"PATH=/usr/bin", "LANG=C"}, nil))
	if got["LC_CTYPE"] != "C.UTF-8" {
		t.Fatalf("LC_CTYPE = %q, want C.UTF-8", got["LC_CTYPE"])
	}
}

func environmentByKey(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if key, value, ok := strings.Cut(kv, "="); ok {
			out[key] = value
		}
	}
	return out
}
