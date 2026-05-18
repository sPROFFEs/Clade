package main

// Smoke tests for the chiko-style layout. We don't drive a full Bubble
// Tea program here — we just call View() / Update() on the layout to
// confirm the persistent chrome renders and key routing wires up.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

func TestLayoutRendersWithoutPanic(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-test"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	_ = l.Init()
	out := l.View()
	if out == "" {
		t.Fatal("layout View() returned empty string")
	}
	for _, want := range []string{"clade", "Navigator", "Chats", "Templates", "Agents", "Help"} {
		if !strings.Contains(out, want) {
			t.Errorf("layout missing %q in render:\n%s", want, out)
		}
	}
}

func TestLayoutSelectNavSwitchesSection(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-test2"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	if l.navCurrent != navSectionChats {
		t.Fatalf("initial nav = %q, want %q", l.navCurrent, navSectionChats)
	}
	// Selecting nav index 1 (Templates) should swap the pane and
	// update navCurrent. We call selectNav directly because Bubble
	// Tea's ctrl-digit key encoding varies between terminal drivers
	// and isn't worth pinning down here.
	_ = l.selectNav(1)
	if l.navCurrent != navSectionTemplates {
		t.Errorf("nav after selectNav(1) = %q, want %q", l.navCurrent, navSectionTemplates)
	}
}

func TestLayoutPaletteOpensOnColon(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-test3"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	next, _ := l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	l = next.(*layoutModel)
	if !l.palette.visible {
		t.Fatal("palette did not open on ':'")
	}
	if l.focus != focusPalette {
		t.Errorf("focus = %v, want focusPalette", l.focus)
	}
}

func TestLayoutPinChatMsgAddsTab(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-test4"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	next, _ := l.Update(pinChatMsg{chatID: "abc", label: "demo"})
	l = next.(*layoutModel)
	if len(l.tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(l.tabs))
	}
	if l.tabs[0].label != "demo" {
		t.Errorf("tab label = %q, want %q", l.tabs[0].label, "demo")
	}
}

