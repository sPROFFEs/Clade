package main

// Pane is the chiko-style content view that renders into the right
// region of the persistent layout. Each screen (chats list, templates,
// agents, settings editor, etc.) implements Pane so the outer
// layoutModel can compose them under one persistent navigator + tab
// strip + statusline.
//
// Panes return their body content WITHOUT outer chrome — the layout
// draws the borders, title bar, and help bar around them. Drill-down
// transitions still use screenDoneMsg{next: ...}; the layout swaps
// the active pane on receipt.

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Pane composes tea.Model with three small accessors the layout
// needs to render its persistent chrome.
//
// Implementations must keep Update returning the same concrete type
// (the layout type-asserts back to Pane); value-typed Bubble Tea
// models satisfy this naturally.
type Pane interface {
	tea.Model

	// Title is what the right-pane title bar should read. Usually
	// includes the section + selected item ("Chat · refactor").
	Title() string

	// Help is the contextual key hint line shown above the
	// statusline. Empty string omits the bar.
	Help() string

	// Body is the body content area. The layout sizes it to the
	// available width/height before rendering.
	Body() string

	// NavSection returns the navigator section this pane belongs
	// to (chats / templates / agents / help / ""). The layout uses
	// it to highlight the current section in the sidebar; drill-
	// down panes return the parent section so the user still sees
	// where they are.
	NavSection() string

	// CapturingInput reports whether this pane currently has a
	// text-input field focused. The layout uses it to stop
	// intercepting "shortcut" keys that overlap with normal text
	// (`:`, `?`) — those defer to the input when true, so the
	// user can actually type a URL like http://… without the
	// palette stealing the colon. ctrl-P always opens the palette
	// regardless; it's the universal fallback.
	CapturingInput() bool
}

// Compile-time assertions — keeps the type system honest when we
// pass concrete screens into screenDoneMsg{next:...}, since the
// layout's Update type-asserts back to Pane to swap.
var (
	_ Pane = chatListModel{}
	_ Pane = templateListModel{}
	_ Pane = newTemplateModel{}
	_ Pane = pickTemplateModel{}
	_ Pane = newChatFromTemplateModel{}
	_ Pane = agentsModel{}
	_ Pane = settingsModel{}
	_ Pane = filesModel{}
	_ Pane = ollamaModel{}
	_ Pane = installModel{}
	_ Pane = launchingModel{}
	_ Pane = helpPaneModel{}
	_ Pane = searchModel{}
	_ Pane = recipesModel{}
)

