package main

// Smoke tests for the chiko-style layout. We don't drive a full Bubble
// Tea program here — we just call View() / Update() on the layout to
// confirm the persistent chrome renders and key routing wires up.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/launcher"
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
	for _, want := range []string{"praimate", "Navigator", "Chats", "Agents", "CLIs", "Automation", "Help"} {
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
	// Selecting nav index 1 (Agents) should swap the pane and
	// update navCurrent. We call selectNav directly because Bubble
	// Tea's ctrl-digit key encoding varies between terminal drivers
	// and isn't worth pinning down here.
	_ = l.selectNav(1)
	if l.navCurrent != navSectionRecipes {
		t.Errorf("nav after selectNav(1) = %q, want %q", l.navCurrent, navSectionRecipes)
	}
}

func TestLayoutPaletteOpensOnCtrlP(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-test3"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	next, _ := l.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	l = next.(*layoutModel)
	if !l.palette.visible {
		t.Fatal("palette did not open on ctrl-p")
	}
	if l.focus != focusPalette {
		t.Errorf("focus = %v, want focusPalette", l.focus)
	}
}

// `:` still opens the palette when the active pane is list-only (no
// text input focused). The chat list is the boot pane in our test
// layout, so it qualifies.
func TestLayoutPaletteOpensOnColonInListPane(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-colon-list"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	next, _ := l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	l = next.(*layoutModel)
	if !l.palette.visible {
		t.Fatal("palette did not open on ':' for a list-only pane")
	}
}

// `:` is forwarded to the pane (does NOT open the palette) when the
// active pane reports CapturingInput()=true. We use a fake pane to
// avoid spinning up an ollama wizard with all its async probing.
func TestLayoutColonDefersToInputPane(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/ws-layout-colon-input"}
	setTermSize(120, 30)
	l := newLayoutModel(cfg)
	l.pane = &fakeCapturingPane{}
	next, _ := l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	l = next.(*layoutModel)
	if l.palette.visible {
		t.Fatal("palette opened on ':' while pane was capturing input")
	}
}

// fakeCapturingPane is a minimal Pane that claims to be capturing
// text — used only to verify the layout's input-aware routing.
type fakeCapturingPane struct{ saw string }

func (p *fakeCapturingPane) Init() tea.Cmd { return nil }
func (p *fakeCapturingPane) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		p.saw += k.String()
	}
	return p, nil
}
func (p *fakeCapturingPane) View() string         { return "" }
func (p *fakeCapturingPane) Title() string        { return "fake" }
func (p *fakeCapturingPane) Help() string         { return "" }
func (p *fakeCapturingPane) Body() string         { return "" }
func (p *fakeCapturingPane) NavSection() string   { return "" }
func (p *fakeCapturingPane) CapturingInput() bool { return true }

// Pin-tab feature removed in 0.1.12 — see git history. The test
// previously here verified that `p` on the chat list emitted a
// pinChatMsg the layout intercepted to add a tab strip entry. With
// the feature gone, neither the message type nor the tab strip
// exists.
