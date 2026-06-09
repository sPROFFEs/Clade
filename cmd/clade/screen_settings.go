package main

// Per-chat (and per-template) settings screen. Previously a 5-step
// linear wizard the user had to walk through every visit; now a single
// list-of-settings the user navigates with up/down and dives into one
// at a time with Enter. Esc from a sub-editor returns to the list;
// Esc from the list returns to the chat list.
//
// Settings list items:
//   1. Language          (textinput)
//   2. Memory            (yes/no toggle)
//   3. Mirror state      (yes/no toggle — Step 3 opt-in)
//   4. Agent             (deployable picker — moved here from the
//                         chat-list `a` key; opens the agent override
//                         flow inline)
//   5. Online skills     (multi-add list editor)
//
// Save happens whenever the user leaves the screen via Esc on the list
// (or implicitly on agent swap — that one writes through immediately
// because changing the agent invalidates resume state).

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sPROFFEs/Clade/internal/installer"
	"github.com/sPROFFEs/Clade/internal/launcher"
	"github.com/sPROFFEs/Clade/pkg/workpath"
)

// settingsItem indexes the rows in the settings list. Keep in sync
// with the items slice in body() — adding/reordering rows means
// touching exactly one switch.
type settingsItem int

const (
	settingsItemLanguage settingsItem = iota
	settingsItemMemory
	settingsItemContextPrimer
	settingsItemMirror
	settingsItemAgent
	settingsItemEndpoint // local OpenAI-compat / Ollama endpoint
	settingsItemSkills
	settingsItemBundles // tool/capability _common/<bundle> imports toggled on this workpath
	settingsItemCount
)

// settingsMode is which sub-editor (if any) is currently visible.
type settingsMode int

const (
	settingsModeList settingsMode = iota
	settingsModeEditLanguage
	settingsModeEditSkills
	settingsModeEditAgent
	settingsModeEditBundles
)

type settingsModel struct {
	cfg *launcher.Config
	ws  launcher.Workspace

	// currentAgent is the chat's locked agent at screen-open time.
	// Tracked here because Workspace itself doesn't carry an agent ID
	// (only Chat does) and we don't want to re-read chat.json on every
	// render.
	currentAgent launcher.AgentID
	// chatID lets us write through agent swaps to chat.json without
	// having to reverse-derive the path each time.
	chatID string

	cursor int // 0..settingsItemCount-1 — current row in the list view
	mode   settingsMode

	// Local copies of every setting; written back to ws.Settings on
	// save. Keeping them separate from ws.Settings lets the user back
	// out of a sub-editor with Esc and lose only the in-progress
	// changes for THAT setting.
	language string
	memory   bool
	mirror   bool
	// primer holds the LOGICAL state ("primer enabled?"); we invert
	// to/from Settings.DisableContextPrimer at load and save time.
	primer bool
	skills []string

	// Sub-editor state.
	textInput   textinput.Model
	skillInput  textinput.Model
	skillsDirty bool

	// agentItems drives the agent sub-picker. Loaded lazily when the
	// user enters that mode (we don't want to block list display on
	// DetectAgents).
	agentItems  []launcher.Agent
	agentCursor int
	agentErr    string

	// Bundles sub-editor state. bundles is the catalog from
	// templates/_common/; bundleActive[name] tracks whether the
	// workpath currently imports it; bundleTools maps a bundle name
	// to the matching managed tool's availability (if any) so the
	// row shows "✓ installed" / "✗ missing — install via Tools tab".
	bundles       []launcher.Bundle
	bundleActive  map[string]bool
	bundleTools   map[string]bool // true when the matching Tool is installed (Available)
	bundleCursor  int
	bundlesErr    string
	bundlesLoaded bool

	// Diagnostic strings shown in the list footer.
	err string
	ok  string
}

func newSettingsModel(cfg *launcher.Config, ws launcher.Workspace) settingsModel {
	lang := textinput.New()
	lang.Placeholder = "blank to clear (e.g. Spanish, ja, Italian)"
	lang.Width = 40
	lang.CharLimit = 60
	lang.SetValue(ws.Settings.Language)

	skill := textinput.New()
	skill.Placeholder = "git URL — Enter to add"
	skill.Width = 70
	skill.CharLimit = 300

	m := settingsModel{
		cfg:        cfg,
		ws:         ws,
		mode:       settingsModeList,
		language:   ws.Settings.Language,
		memory:     ws.Settings.MemoryEnabled,
		mirror:     ws.Settings.MirrorAgentState,
		primer:     !ws.Settings.DisableContextPrimer, // primer ON by default
		skills:     append([]string(nil), ws.Settings.OnlineSkills...),
		textInput:  lang,
		skillInput: skill,
	}
	// Resolve the chat behind this Workspace (if any) so we can show
	// + edit the locked agent. Templates don't have an AgentID so we
	// leave the field empty and the agent row reads as "(none)".
	if id := chatIDFromRoot(ws.Root); id != "" {
		m.chatID = id
		if chat, err := launcher.LoadChat(cfg.WorkspacesRoot, id); err == nil && chat != nil {
			m.currentAgent = chat.AgentID
		}
	}
	return m
}

// chatIDFromRoot reverse-derives the chat ID from <root>/chats/<id>.
// Empty if ws.Root doesn't look like a chat path (e.g. it's a template).
func chatIDFromRoot(root string) string {
	parent := filepath.Base(filepath.Dir(root))
	if parent != launcher.ChatsDir {
		return ""
	}
	return filepath.Base(root)
}

// freshWorkspace re-reads the workspace/chat/template from disk so a
// downstream screen (e.g. ollamaModel) that wrote settings into
// chat.json doesn't leave the in-memory ws stale when we return.
func freshWorkspace(cfg *launcher.Config, ws launcher.Workspace) launcher.Workspace {
	if id := chatIDFromRoot(ws.Root); id != "" {
		if chat, err := launcher.LoadChat(cfg.WorkspacesRoot, id); err == nil && chat != nil {
			return chat.AsWorkspace()
		}
	}
	if tpl, err := launcher.LoadTemplate(cfg.WorkspacesRoot, ws.Name); err == nil && tpl != nil {
		return launcher.Workspace{
			Name: tpl.Name, Root: tpl.Root, WorkpathDir: tpl.WorkpathDir,
			Description: tpl.Description, Settings: tpl.Settings,
		}
	}
	return ws
}

// agentsLoadedForSettingsMsg is dispatched when the agent picker's
// async detection finishes. Distinct from agentsLoadedMsg so it can't
// be confused with the main agents picker.
type agentsLoadedForSettingsMsg struct{ items []launcher.Agent }

func (m settingsModel) Init() tea.Cmd { return nil }

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case agentsLoadedForSettingsMsg:
		// Sort available-first; cursor seeds on the chat's current agent.
		items := append([]launcher.Agent(nil), msg.items...)
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Available != items[j].Available {
				return items[i].Available
			}
			return false
		})
		m.agentItems = items
		for i, a := range m.agentItems {
			if a.ID == m.currentAgent {
				m.agentCursor = i
				break
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case settingsModeList:
			return m.updateList(msg)
		case settingsModeEditLanguage:
			return m.updateEditLanguage(msg)
		case settingsModeEditSkills:
			return m.updateEditSkills(msg)
		case settingsModeEditAgent:
			return m.updateEditAgent(msg)
		case settingsModeEditBundles:
			return m.updateEditBundles(msg)
		}
	}
	// Forward unhandled messages to the focused sub-editor for cursor
	// blink etc.
	var cmd tea.Cmd
	switch m.mode {
	case settingsModeEditLanguage:
		m.textInput, cmd = m.textInput.Update(msg)
	case settingsModeEditSkills:
		m.skillInput, cmd = m.skillInput.Update(msg)
	}
	return m, cmd
}

// --- list view -------------------------------------------------------------

func (m settingsModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Save-on-exit: persist any inline-edited values (toggles,
		// agent swap is saved separately). If nothing changed,
		// SaveWorkspaceLikeSettings is still cheap (atomic rewrite),
		// so we don't bother dirty-tracking.
		m.ws.Settings.Language = m.language
		m.ws.Settings.MemoryEnabled = m.memory
		m.ws.Settings.MirrorAgentState = m.mirror
		m.ws.Settings.DisableContextPrimer = !m.primer
		m.ws.Settings.OnlineSkills = m.skills
		if err := launcher.SaveWorkspaceLikeSettings(m.ws); err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m, wrap(newChatListModel(m.cfg))
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < int(settingsItemCount)-1 {
			m.cursor++
		}
	case " ":
		// Space toggles boolean items inline without opening an editor.
		switch settingsItem(m.cursor) {
		case settingsItemMemory:
			m.memory = !m.memory
		case settingsItemMirror:
			m.mirror = !m.mirror
		case settingsItemContextPrimer:
			m.primer = !m.primer
		}
	case "enter":
		switch settingsItem(m.cursor) {
		case settingsItemLanguage:
			m.mode = settingsModeEditLanguage
			m.textInput.SetValue(m.language)
			m.textInput.Focus()
			return m, textinput.Blink
		case settingsItemMemory:
			m.memory = !m.memory
		case settingsItemMirror:
			m.mirror = !m.mirror
		case settingsItemContextPrimer:
			m.primer = !m.primer
		case settingsItemAgent:
			m.mode = settingsModeEditAgent
			m.agentErr = ""
			return m, m.loadAgentsCmd()
		case settingsItemEndpoint:
			// Local endpoint (Ollama / OpenAI-compat) config. Hand off
			// to the existing multi-step wizard but route Esc/apply
			// back here instead of to the chat list — keeps the user
			// inside settings while they tweak related options. Also
			// pre-tick the chat's locked agent so the user doesn't
			// have to remember to check that box (forgetting it
			// silently writes the chat-level setting but skips the
			// agent's own config file, leaving Plan() with a stale
			// `-p ollama_remote` and no profile to find).
			cfg := m.cfg
			ws := m.ws
			currentAgent := m.currentAgent
			return m, wrap(func() ollamaModel {
				om := newOllamaModelWithReturn(cfg, ws, func() tea.Cmd {
					reloaded := newSettingsModel(cfg, freshWorkspace(cfg, ws))
					return wrap(reloaded)
				})
				preTickAgentForChat(&om, currentAgent)
				return om
			}())
		case settingsItemSkills:
			m.mode = settingsModeEditSkills
			m.skillInput.SetValue("")
			m.skillInput.Focus()
			return m, textinput.Blink
		case settingsItemBundles:
			m.mode = settingsModeEditBundles
			m.loadBundles()
			return m, nil
		}
	}
	return m, nil
}

// --- edit: language --------------------------------------------------------

func (m settingsModel) updateEditLanguage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = settingsModeList
		m.textInput.Blur()
		return m, nil
	case tea.KeyEnter:
		m.language = strings.TrimSpace(m.textInput.Value())
		m.mode = settingsModeList
		m.textInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// --- edit: skills ----------------------------------------------------------

func (m settingsModel) updateEditSkills(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = settingsModeList
		m.skillInput.Blur()
		return m, nil
	case "ctrl+d":
		if len(m.skills) > 0 {
			m.skills = m.skills[:len(m.skills)-1]
			m.skillsDirty = true
		}
		return m, nil
	case "enter":
		url := strings.TrimSpace(m.skillInput.Value())
		if url == "" {
			// Blank Enter exits back to the list.
			m.mode = settingsModeList
			m.skillInput.Blur()
			return m, nil
		}
		m.skills = append(m.skills, url)
		m.skillsDirty = true
		m.skillInput.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.skillInput, cmd = m.skillInput.Update(msg)
	return m, cmd
}

// --- edit: agent picker ----------------------------------------------------

func (m settingsModel) loadAgentsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		return agentsLoadedForSettingsMsg{items: launcher.DetectAgents(ctx)}
	}
}

func (m settingsModel) updateEditAgent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = settingsModeList
		return m, nil
	case "up", "k":
		if m.agentCursor > 0 {
			m.agentCursor--
		}
	case "down", "j":
		if m.agentCursor < len(m.agentItems)-1 {
			m.agentCursor++
		}
	case "i":
		if m.agentCursor < len(m.agentItems) {
			return m, wrap(newInstallModel(m.cfg, m.ws, m.agentItems[m.agentCursor].ID))
		}
	case "enter":
		if m.agentCursor >= len(m.agentItems) {
			return m, nil
		}
		a := m.agentItems[m.agentCursor]
		if !a.Available {
			return m, wrap(newInstallModel(m.cfg, m.ws, a.ID))
		}
		// Agent swap is a discrete commit point — write through to
		// chat.json immediately so the next launch reflects the new
		// agent even if the user backs out before Esc'ing settings.
		if m.chatID == "" {
			// Settings on a template, not a chat. Nothing to write.
			m.mode = settingsModeList
			m.ok = "agent picker only applies to chats; templates inherit per-chat"
			return m, nil
		}
		chat, err := launcher.LoadChat(m.cfg.WorkspacesRoot, m.chatID)
		if err != nil || chat == nil {
			m.agentErr = "couldn't reload chat to save agent swap"
			return m, nil
		}
		chat.AgentID = a.ID
		if err := launcher.SaveChatSettings(*chat); err != nil {
			m.agentErr = err.Error()
			return m, nil
		}
		m.currentAgent = a.ID
		m.mode = settingsModeList
		m.ok = "agent set to " + string(a.ID) + " (saved to chat.json)"
	}
	return m, nil
}

// --- rendering -------------------------------------------------------------

func (m settingsModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m settingsModel) Title() string {
	switch m.mode {
	case settingsModeEditLanguage:
		return fmt.Sprintf("Settings · %s · language", m.ws.Name)
	case settingsModeEditSkills:
		return fmt.Sprintf("Settings · %s · online skills", m.ws.Name)
	case settingsModeEditAgent:
		return fmt.Sprintf("Settings · %s · agent", m.ws.Name)
	case settingsModeEditBundles:
		return fmt.Sprintf("Settings · %s · bundles", m.ws.Name)
	}
	return fmt.Sprintf("Settings · %s", m.ws.Name)
}

func (m settingsModel) Help() string {
	switch m.mode {
	case settingsModeList:
		return "↑/↓ select · enter edit / toggle · space toggle · esc save & back"
	case settingsModeEditLanguage:
		return "enter accept · esc cancel"
	case settingsModeEditSkills:
		return "enter add · blank enter finish · ctrl-d remove last · esc back"
	case settingsModeEditAgent:
		return "↑/↓ select · enter swap+launch / install · i install · esc back"
	case settingsModeEditBundles:
		return "↑/↓ select · space or enter toggle · esc back"
	}
	return "esc back"
}

func (m settingsModel) NavSection() string { return navSectionChats }
func (m settingsModel) CapturingInput() bool {
	return m.mode == settingsModeEditLanguage || m.mode == settingsModeEditSkills
}

func (m settingsModel) Body() string {
	switch m.mode {
	case settingsModeEditLanguage:
		var b strings.Builder
		b.WriteString(inputLabelStyle.Render("Default language: "))
		b.WriteString(m.textInput.View() + "\n\n")
		b.WriteString(descStyle.Render("Added as 'respond in <lang>' directive at launch."))
		return b.String()

	case settingsModeEditSkills:
		var b strings.Builder
		b.WriteString(inputLabelStyle.Render("Online skill URL: "))
		b.WriteString(m.skillInput.View() + "\n\n")
		b.WriteString(descStyle.Render("Enter a git URL to add · blank Enter to finish · ctrl-d removes last") + "\n\n")
		if len(m.skills) == 0 {
			b.WriteString(descStyle.Render("(no skills yet)"))
		} else {
			for i, u := range m.skills {
				b.WriteString(descStyle.Render(fmt.Sprintf("  %d. %s", i+1, u)) + "\n")
			}
		}
		return b.String()

	case settingsModeEditAgent:
		return m.renderAgentPicker()

	case settingsModeEditBundles:
		return m.renderBundlesEditor()
	}

	// List view.
	return m.renderList()
}

func (m settingsModel) renderList() string {
	var b strings.Builder
	rows := []struct {
		label string
		value string
		hint  string
	}{
		{"Language", emptyValue(m.language, "(none)"), "Prepend a 'respond in <lang>' directive to the agent's first turn."},
		{"Persistent MEMORY.md", boolValue(m.memory), "Stage a MEMORY.md the agent can read/write across sessions."},
		{"Context primer", boolValue(m.primer), "On every fresh launch, send a short prompt telling the agent to read MEMORY.md / playbook / rules and ack with 'Context loaded'. ON by default; turn off if your agent's own auto-load is enough and the ack is noise."},
		{"Mirror agent state", boolValue(m.mirror), "Restore the chat's captured slice into the agent's home dir before launch. SIGKILL-safe via mtime comparison. Snapshot on exit always runs regardless."},
		{"Agent", agentLabel(m.currentAgent), "Press Enter to open the per-chat agent picker. Switching agents writes through to chat.json immediately."},
		{"Local endpoint", endpointLabel(m.ws.Settings.Ollama), "Route this chat through an OpenAI-compatible local endpoint (Ollama, GPUStack, vLLM, …) instead of the agent's vendor API."},
		{"Online skills", fmt.Sprintf("%d", len(m.skills)), "Git URLs fetched into the sandbox's .claude/skills/ on launch."},
		{"Tool bundles", bundlesValue(m), "Select which installed tools/capability bundles are active for this chat or template. Enabled bundles inject wrappers, knowledge, and playbook/rules instructions so the model is told the tools exist and when to use them."},
	}
	for i, r := range rows {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		left := marker + r.label
		// Pad label to a fixed width so values right-align.
		pad := 24 - renderListLabel(left)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(selectionRow(left+strings.Repeat(" ", pad)+r.value, isSel) + "\n")
		if isSel {
			b.WriteString(descStyle.Render("   "+r.hint) + "\n")
		}
	}
	if m.ok != "" {
		b.WriteString("\n" + okStyle.Render("✓ "+m.ok))
	}
	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return b.String()
}

func (m settingsModel) renderAgentPicker() string {
	var b strings.Builder
	if len(m.agentItems) == 0 {
		b.WriteString(hintStyle.Render("Scanning PATH for agent CLIs..."))
		return b.String()
	}
	b.WriteString(hintStyle.Render(
		"Switch which agent runs this chat. Enter on an installed one swaps + launches; "+
			"Enter on a missing one routes to install.") + "\n\n")
	for i, a := range m.agentItems {
		isSel := i == m.agentCursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		label := a.Label
		statusText := availableStyle.Render("● available")
		if !a.Available {
			statusText = missingStyle.Render("○ not installed")
			if a.ProbeError != "" {
				statusText = errorStyle.Render("✗ broken install")
			}
		}
		line := marker + label + "   " + statusText
		if a.Version != "" {
			line += "  " + lipglossDimRender("("+a.Version+")", isSel)
		}
		b.WriteString(selectionRow(line, isSel) + "\n")
		if isSel && !a.Available && a.InstallHint != "" {
			b.WriteString(descStyle.Render("  install/repair: "+a.InstallHint) + "\n")
		}
	}
	if m.agentErr != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.agentErr))
	}
	return b.String()
}

// --- small helpers (kept package-private to avoid leaking from chrome) ----

func emptyValue(s, fallback string) string {
	if s == "" {
		return descStyle.Render(fallback)
	}
	return s
}

func boolValue(b bool) string {
	if b {
		return availableStyle.Render("[x] on")
	}
	return descStyle.Render("[ ] off")
}

func agentLabel(id launcher.AgentID) string {
	if id == "" {
		return descStyle.Render("(none)")
	}
	return string(id)
}

// endpointLabel renders the current Ollama / local-endpoint config in
// one line for the settings menu's value column. Returns a dim "(off)"
// when nothing's configured.
func endpointLabel(s launcher.OllamaSettings) string {
	if s.Endpoint == "" && s.Model == "" {
		return descStyle.Render("(off)")
	}
	model := s.Model
	if model == "" {
		model = "?"
	}
	ep := s.Endpoint
	if ep == "" {
		ep = "?"
	}
	return model + "  " + descStyle.Render("@ "+ep)
}

// renderListLabel returns a wrapper around lipgloss.Width because the
// rest of the file deals with marker-prefixed labels; centralising the
// width call keeps adjustments to padding consistent.
func renderListLabel(s string) int { return lipgloss.Width(s) }

// --- bundles sub-editor ---------------------------------------------------

// loadBundles reads the available _common/<bundle> directories AND the
// workpath's currently-active imports, then probes installer.DetectTools
// to mark whether each bundle's underlying tool is installed. Idempotent
// — calling twice is safe; the second call refreshes from disk.
func (m *settingsModel) loadBundles() {
	m.bundles = nil
	m.bundleActive = map[string]bool{}
	m.bundleTools = map[string]bool{}
	m.bundleCursor = 0
	m.bundlesErr = ""
	m.bundlesLoaded = true

	bundles, err := launcher.DiscoverBundles(m.cfg.WorkspacesRoot)
	if err != nil {
		m.bundlesErr = err.Error()
		return
	}
	m.bundles = bundles

	// What's currently imported by this workpath?
	wpJSON := filepath.Join(m.ws.WorkpathDir, "workpath.json")
	current, err := workpath.ReadImports(wpJSON)
	if err != nil {
		m.bundlesErr = err.Error()
		return
	}
	// Mark active by basename match. We can't compare verbatim because
	// the stored path varies by depth (_common/foo vs ../_common/foo
	// vs ../../templates/_common/foo); the basename is stable.
	for _, ref := range current {
		base := filepath.Base(filepath.FromSlash(ref))
		m.bundleActive[base] = true
	}

	// Probe installed tools so each row shows availability. Short
	// timeout — this runs synchronously when the user enters the sub-
	// editor; can't block more than a second or two without bad UX.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, t := range installer.DetectTools(ctx) {
		m.bundleTools[string(t.ID)] = t.Available
	}
}

func (m settingsModel) updateEditBundles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = settingsModeList
		return m, nil
	case "up", "k":
		if m.bundleCursor > 0 {
			m.bundleCursor--
		}
	case "down", "j":
		if m.bundleCursor < len(m.bundles)-1 {
			m.bundleCursor++
		}
	case " ", "enter":
		if m.bundleCursor >= len(m.bundles) {
			return m, nil
		}
		b := m.bundles[m.bundleCursor]
		wpJSON := filepath.Join(m.ws.WorkpathDir, "workpath.json")
		if m.bundleActive[b.Name] {
			// Remove EVERY variant of this bundle's import path (we
			// stored by basename match, so the user may have used
			// _common/x or ../_common/x — strip them all).
			current, _ := workpath.ReadImports(wpJSON)
			for _, ref := range current {
				if filepath.Base(filepath.FromSlash(ref)) == b.Name {
					if err := workpath.RemoveImport(wpJSON, ref); err != nil {
						m.bundlesErr = err.Error()
						return m, nil
					}
				}
			}
			m.bundleActive[b.Name] = false
		} else {
			ref, err := launcher.RelativeImportPath(m.ws.WorkpathDir, m.cfg.WorkspacesRoot, b.Name)
			if err != nil {
				m.bundlesErr = err.Error()
				return m, nil
			}
			if err := workpath.AddImport(wpJSON, ref); err != nil {
				m.bundlesErr = err.Error()
				return m, nil
			}
			m.bundleActive[b.Name] = true
		}
		m.bundlesErr = ""
	}
	return m, nil
}

func (m settingsModel) renderBundlesEditor() string {
	var b strings.Builder
	if !m.bundlesLoaded {
		b.WriteString(hintStyle.Render("Scanning templates/_common/..."))
		return b.String()
	}
	if len(m.bundles) == 0 {
		b.WriteString(hintStyle.Render(
			"No tool bundles registered. Drop a directory under " +
				"templates/_common/<name>/ with a playbook-fragment.md to activate one."))
		if m.bundlesErr != "" {
			b.WriteString("\n\n" + errorStyle.Render("✗ "+m.bundlesErr))
		}
		return b.String()
	}
	b.WriteString(hintStyle.Render(
		"Select tool bundles to activate for this chat/template. Enabled bundles "+
			"merge wrappers, knowledge, hooks, and playbook/rules fragments into the "+
			"sandbox at compile time, explicitly telling the model the tool exists "+
			"and when to use it. Missing tools can be installed from the Tools tab "+
			"(Ctrl-4).") + "\n\n")

	for i, bun := range m.bundles {
		isSel := i == m.bundleCursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		check := "[ ]"
		if m.bundleActive[bun.Name] {
			check = availableStyle.Render("[x]")
		}
		// Tool installed status: only show when there's a matching
		// known Tool of the same name.
		status := ""
		if installed, known := m.bundleTools[bun.Name]; known {
			if installed {
				status = "   " + availableStyle.Render("✓ tool installed")
			} else {
				status = "   " + errorStyle.Render("✗ tool missing")
			}
		}
		line := marker + check + " " + bun.Title + status
		b.WriteString(selectionRow(line, isSel) + "\n")
		if isSel && bun.Description != "" {
			b.WriteString(descStyle.Render("  "+bun.Description) + "\n")
		}
		if isSel && bun.Description == "" {
			b.WriteString(descStyle.Render("  (no description — bundle has no playbook-fragment.md heading)") + "\n")
		}
	}
	if m.bundlesErr != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.bundlesErr))
	}
	return b.String()
}

// bundlesValue is the right-column text for the Tool bundles row in the
// main list. Counts how many bundles this workpath imports, falling
// back to "(none)" when zero. Reads from disk (cheap) so it stays
// fresh after the sub-editor mutates the manifest.
func bundlesValue(m settingsModel) string {
	wpJSON := filepath.Join(m.ws.WorkpathDir, "workpath.json")
	current, err := workpath.ReadImports(wpJSON)
	if err != nil || len(current) == 0 {
		return descStyle.Render("(none)")
	}
	return fmt.Sprintf("%d active", len(current))
}
