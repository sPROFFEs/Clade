package main

// One-off "render to stdout" inspection — not a real test. Skipped
// unless RUN_LAYOUT_DUMP=1 is set. Helps eyeball the layout offline.

import (
	"os"
	"testing"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

func TestDumpLayout(t *testing.T) {
	if os.Getenv("RUN_LAYOUT_DUMP") != "1" {
		t.Skip("set RUN_LAYOUT_DUMP=1 to dump rendered layout")
	}
	cfg := &launcher.Config{WorkspacesRoot: "/home/parrot/clade-workspaces", LastAgent: "claude"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	_ = l.Init()
	os.Stdout.WriteString("\n=== layout at 120x30 ===\n")
	os.Stdout.WriteString(l.View())
	os.Stdout.WriteString("\n=== focus=nav, navCursor=2 ===\n")
	l.focus = focusNav
	l.navCursor = 2
	os.Stdout.WriteString(l.View())
	os.Stdout.WriteString("\n=== with two tabs ===\n")
	l.tabs = []chatTab{{chatID: "a", label: "refactor-x"}, {chatID: "b", label: "docs"}}
	l.activeTab = 0
	l.focus = focusPane
	os.Stdout.WriteString(l.View())
	os.Stdout.WriteString("\n=== with palette open ===\n")
	l.openPalette()
	l.palette.input.SetValue("new")
	os.Stdout.WriteString(l.View())
}
