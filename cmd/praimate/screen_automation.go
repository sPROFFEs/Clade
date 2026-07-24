package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

type automationTab int

const (
	automationTabWatchers automationTab = iota
	automationTabSchedules
	automationTabPrivacy
)

type automationMode int

const (
	automationModeList automationMode = iota
	automationModeAddWatcher
	automationModeAddSchedule
	automationModeAddPrivacy
)

type automationModel struct {
	cfg *launcher.Config

	tab    automationTab
	mode   automationMode
	cursor int
	field  int

	loaded    bool
	err       string
	status    string
	agents    []core.Agent
	watchers  []core.Watcher
	schedules []core.Schedule
	patterns  []string

	watcherInputs  []textinput.Model
	scheduleInputs []textinput.Model
	privacyInput   textinput.Model
}

func newAutomationModel(cfg *launcher.Config) automationModel {
	return automationModel{
		cfg:            cfg,
		watcherInputs:  newAutomationInputs([]string{"agent id", "path", "patterns (*.go,internal/*.go)", "workflow (optional)"}),
		scheduleInputs: newAutomationInputs([]string{"agent id", "cron (* * * * *)", "at RFC3339 (optional)", "workflow (optional)"}),
		privacyInput:   newAutomationInput("custom regex"),
	}
}

type automationLoadedMsg struct {
	agents    []core.Agent
	watchers  []core.Watcher
	schedules []core.Schedule
	patterns  []string
	err       error
}

type automationActionMsg struct {
	status string
	err    error
}

func (m automationModel) Init() tea.Cmd {
	return func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return automationLoadedMsg{err: fmtCoreInitErr()}
		}
		ctx := context.Background()
		agents, err := c.ListAgents(ctx)
		if err != nil {
			return automationLoadedMsg{err: err}
		}
		watchers, err := c.ListWatchers(ctx, false)
		if err != nil {
			return automationLoadedMsg{err: err}
		}
		schedules, err := c.ListSchedules(ctx, false)
		if err != nil {
			return automationLoadedMsg{err: err}
		}
		patterns, err := c.ListPrivacyPatterns(ctx)
		if err != nil {
			return automationLoadedMsg{err: err}
		}
		return automationLoadedMsg{agents: agents, watchers: watchers, schedules: schedules, patterns: patterns}
	}
}

func (m automationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case automationLoadedMsg:
		m.loaded = true
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.agents = msg.agents
		m.watchers = msg.watchers
		m.schedules = msg.schedules
		m.patterns = msg.patterns
		m.clampCursor()
		return m, nil
	case automationActionMsg:
		m.status = msg.status
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.mode = automationModeList
		m.field = 0
		m.resetInputs()
		return m, m.Init()
	case tea.KeyMsg:
		if m.mode != automationModeList {
			return m.updateForm(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m automationModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		if m.tab > automationTabWatchers {
			m.tab--
			m.cursor = 0
		}
	case "right", "l":
		if m.tab < automationTabPrivacy {
			m.tab++
			m.cursor = 0
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.rowCount()-1 {
			m.cursor++
		}
	case "r":
		m.loaded = false
		return m, m.Init()
	case "a":
		return m.startAdd()
	case "enter":
		return m.toggleSelected()
	case "d":
		return m.deleteSelected()
	}
	return m, nil
}

func (m automationModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = automationModeList
		m.field = 0
		m.resetInputs()
		return m, nil
	case "enter":
		if m.field < m.formFieldCount()-1 {
			m.blurField()
			m.field++
			m.focusField()
			return m, textinput.Blink
		}
		return m.submitForm()
	case "tab", "down":
		if m.field < m.formFieldCount()-1 {
			m.blurField()
			m.field++
			m.focusField()
		}
	case "shift+tab", "up":
		if m.field > 0 {
			m.blurField()
			m.field--
			m.focusField()
		}
	default:
		var cmd tea.Cmd
		switch m.mode {
		case automationModeAddWatcher:
			m.watcherInputs[m.field], cmd = m.watcherInputs[m.field].Update(msg)
		case automationModeAddSchedule:
			m.scheduleInputs[m.field], cmd = m.scheduleInputs[m.field].Update(msg)
		case automationModeAddPrivacy:
			m.privacyInput, cmd = m.privacyInput.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

func (m automationModel) startAdd() (tea.Model, tea.Cmd) {
	m.field = 0
	switch m.tab {
	case automationTabWatchers:
		m.mode = automationModeAddWatcher
		if len(m.agents) > 0 {
			m.watcherInputs[0].SetValue(m.agents[0].ID)
		}
	case automationTabSchedules:
		m.mode = automationModeAddSchedule
		if len(m.agents) > 0 {
			m.scheduleInputs[0].SetValue(m.agents[0].ID)
		}
	case automationTabPrivacy:
		m.mode = automationModeAddPrivacy
	}
	m.focusField()
	return m, textinput.Blink
}

func (m automationModel) submitForm() (tea.Model, tea.Cmd) {
	switch m.mode {
	case automationModeAddWatcher:
		agentID := strings.TrimSpace(m.watcherInputs[0].Value())
		path := strings.TrimSpace(m.watcherInputs[1].Value())
		patterns := splitCSV(m.watcherInputs[2].Value())
		workflow := strings.TrimSpace(m.watcherInputs[3].Value())
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			if _, err := c.AddWatcher(context.Background(), core.AddWatcherRequest{
				AgentID: agentID, Path: filepath.Clean(path), Patterns: patterns, Workflow: workflow,
			}); err != nil {
				return automationActionMsg{err: err}
			}
			restartAppWatchers()
			return automationActionMsg{status: "added watcher"}
		}
	case automationModeAddSchedule:
		agentID := strings.TrimSpace(m.scheduleInputs[0].Value())
		cronExpr := strings.TrimSpace(m.scheduleInputs[1].Value())
		atText := strings.TrimSpace(m.scheduleInputs[2].Value())
		workflow := strings.TrimSpace(m.scheduleInputs[3].Value())
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			req := core.AddScheduleRequest{AgentID: agentID, Cron: cronExpr, Workflow: workflow}
			if atText != "" {
				at, err := time.Parse(time.RFC3339, atText)
				if err != nil {
					return automationActionMsg{err: err}
				}
				req.Cron = ""
				req.At = &at
			}
			if _, err := c.AddSchedule(context.Background(), req); err != nil {
				return automationActionMsg{err: err}
			}
			return automationActionMsg{status: "added schedule"}
		}
	case automationModeAddPrivacy:
		pattern := m.privacyInput.Value()
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			if err := c.AddPrivacyPattern(context.Background(), pattern); err != nil {
				return automationActionMsg{err: err}
			}
			return automationActionMsg{status: "added privacy pattern"}
		}
	}
	return m, nil
}

func (m automationModel) toggleSelected() (tea.Model, tea.Cmd) {
	switch m.tab {
	case automationTabWatchers:
		if m.cursor >= len(m.watchers) {
			return m, nil
		}
		w := m.watchers[m.cursor]
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			next := !w.Enabled
			if err := c.SetWatcherEnabled(context.Background(), w.ID, next); err != nil {
				return automationActionMsg{err: err}
			}
			restartAppWatchers()
			return automationActionMsg{status: toggleStatus("watcher", w.ID, next)}
		}
	case automationTabSchedules:
		if m.cursor >= len(m.schedules) {
			return m, nil
		}
		s := m.schedules[m.cursor]
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			next := !s.Enabled
			if err := c.SetScheduleEnabled(context.Background(), s.ID, next); err != nil {
				return automationActionMsg{err: err}
			}
			return automationActionMsg{status: toggleStatus("schedule", s.ID, next)}
		}
	}
	return m, nil
}

func (m automationModel) deleteSelected() (tea.Model, tea.Cmd) {
	switch m.tab {
	case automationTabWatchers:
		if m.cursor >= len(m.watchers) {
			return m, nil
		}
		id := m.watchers[m.cursor].ID
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			if err := c.DeleteWatcher(context.Background(), id); err != nil {
				return automationActionMsg{err: err}
			}
			restartAppWatchers()
			return automationActionMsg{status: fmt.Sprintf("deleted watcher %d", id)}
		}
	case automationTabSchedules:
		if m.cursor >= len(m.schedules) {
			return m, nil
		}
		id := m.schedules[m.cursor].ID
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			if err := c.DeleteSchedule(context.Background(), id); err != nil {
				return automationActionMsg{err: err}
			}
			return automationActionMsg{status: fmt.Sprintf("deleted schedule %d", id)}
		}
	case automationTabPrivacy:
		if m.cursor >= len(m.patterns) {
			return m, nil
		}
		index := m.cursor
		return m, func() tea.Msg {
			c := getAppCore()
			if c == nil {
				return automationActionMsg{err: fmtCoreInitErr()}
			}
			if err := c.DeletePrivacyPattern(context.Background(), index); err != nil {
				return automationActionMsg{err: err}
			}
			return automationActionMsg{status: "deleted privacy pattern"}
		}
	}
	return m, nil
}

func (m automationModel) View() string { return renderChrome(m.Title(), m.Body(), m.Help()) }
func (m automationModel) Title() string {
	switch m.tab {
	case automationTabSchedules:
		return "Automation · schedules"
	case automationTabPrivacy:
		return "Automation · privacy"
	default:
		return "Automation · watchers"
	}
}
func (m automationModel) NavSection() string   { return navSectionAutomation }
func (m automationModel) CapturingInput() bool { return m.mode != automationModeList }

func (m automationModel) Help() string {
	if m.mode != automationModeList {
		return "tab/↑↓ next field · enter submit · esc cancel"
	}
	if m.tab == automationTabPrivacy {
		return "h/l switch tab · a add · d delete · r reload · ctrl-c quit"
	}
	return "h/l switch tab · a add · enter toggle · d delete · r reload · ctrl-c quit"
}

func (m automationModel) Body() string {
	if !m.loaded {
		return descStyle.Render("loading automation settings...")
	}
	var b strings.Builder
	b.WriteString(m.renderTabs() + "\n\n")
	if m.err != "" {
		b.WriteString(errorStyle.Render("error: "+m.err) + "\n\n")
	}
	if m.status != "" {
		b.WriteString(okStyle.Render(m.status) + "\n\n")
	}
	if m.mode != automationModeList {
		return b.String() + m.bodyForm()
	}
	switch m.tab {
	case automationTabSchedules:
		return b.String() + m.bodySchedules()
	case automationTabPrivacy:
		return b.String() + m.bodyPrivacy()
	default:
		return b.String() + m.bodyWatchers()
	}
}

func (m automationModel) renderTabs() string {
	labels := []string{"Watchers", "Schedules", "Privacy"}
	parts := make([]string, len(labels))
	for i, label := range labels {
		if automationTab(i) == m.tab {
			parts[i] = okStyle.Render(label)
		} else {
			parts[i] = descStyle.Render(label)
		}
	}
	return strings.Join(parts, descStyle.Render("  |  "))
}

func (m automationModel) bodyWatchers() string {
	if len(m.watchers) == 0 {
		return descStyle.Render("(no watchers)")
	}
	var b strings.Builder
	for i, w := range m.watchers {
		state := "off"
		if w.Enabled {
			state = "on"
		}
		row := fmt.Sprintf("%s%-4d %-8s %-18s %-10s %s", rowMarker(i == m.cursor), w.ID, state, w.AgentID, w.Workflow, w.Path)
		b.WriteString(selectionRow(row, i == m.cursor) + "\n")
		if i == m.cursor && len(w.Patterns) > 0 {
			b.WriteString(descStyle.Render("  patterns: "+strings.Join(w.Patterns, ", ")) + "\n")
		}
	}
	return b.String()
}

func (m automationModel) bodySchedules() string {
	if len(m.schedules) == 0 {
		return descStyle.Render("(no schedules)")
	}
	var b strings.Builder
	for i, s := range m.schedules {
		state := "off"
		if s.Enabled {
			state = "on"
		}
		trigger := s.Cron
		if trigger == "" && s.At != nil {
			trigger = s.At.Format(time.RFC3339)
		}
		row := fmt.Sprintf("%s%-4d %-8s %-18s %-10s %s", rowMarker(i == m.cursor), s.ID, state, s.AgentID, s.Workflow, trigger)
		b.WriteString(selectionRow(row, i == m.cursor) + "\n")
		if i == m.cursor && s.NextRunAt != nil {
			b.WriteString(descStyle.Render("  next: "+s.NextRunAt.Format(time.RFC3339)) + "\n")
		}
	}
	return b.String()
}

func (m automationModel) bodyPrivacy() string {
	if len(m.patterns) == 0 {
		return descStyle.Render("(no custom privacy patterns)")
	}
	var b strings.Builder
	for i, pattern := range m.patterns {
		row := fmt.Sprintf("%s%-4d %s", rowMarker(i == m.cursor), i+1, pattern)
		b.WriteString(selectionRow(row, i == m.cursor) + "\n")
	}
	return b.String()
}

func (m automationModel) bodyForm() string {
	switch m.mode {
	case automationModeAddWatcher:
		return formBody("Add watcher", []string{"Agent ID", "Path", "Patterns", "Workflow"}, m.watcherInputs, m.field)
	case automationModeAddSchedule:
		return formBody("Add schedule", []string{"Agent ID", "Cron", "At", "Workflow"}, m.scheduleInputs, m.field)
	case automationModeAddPrivacy:
		return subtitleStyle.Render("Add privacy pattern") + "\n" + m.privacyInput.View() + "\n"
	}
	return ""
}

func formBody(title string, labels []string, inputs []textinput.Model, field int) string {
	var b strings.Builder
	b.WriteString(subtitleStyle.Render(title) + "\n")
	for i, input := range inputs {
		marker := "  "
		if i == field {
			marker = "> "
		}
		b.WriteString(marker + okStyle.Render(labels[i]) + "\n")
		b.WriteString("  " + input.View() + "\n\n")
	}
	return b.String()
}

func (m automationModel) rowCount() int {
	switch m.tab {
	case automationTabSchedules:
		return len(m.schedules)
	case automationTabPrivacy:
		return len(m.patterns)
	default:
		return len(m.watchers)
	}
}

func (m *automationModel) clampCursor() {
	if m.cursor >= m.rowCount() {
		m.cursor = m.rowCount() - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m automationModel) formFieldCount() int {
	switch m.mode {
	case automationModeAddWatcher:
		return len(m.watcherInputs)
	case automationModeAddSchedule:
		return len(m.scheduleInputs)
	case automationModeAddPrivacy:
		return 1
	default:
		return 0
	}
}

func (m *automationModel) focusField() {
	switch m.mode {
	case automationModeAddWatcher:
		m.watcherInputs[m.field].Focus()
	case automationModeAddSchedule:
		m.scheduleInputs[m.field].Focus()
	case automationModeAddPrivacy:
		m.privacyInput.Focus()
	}
}

func (m *automationModel) blurField() {
	switch m.mode {
	case automationModeAddWatcher:
		m.watcherInputs[m.field].Blur()
	case automationModeAddSchedule:
		m.scheduleInputs[m.field].Blur()
	case automationModeAddPrivacy:
		m.privacyInput.Blur()
	}
}

func (m *automationModel) resetInputs() {
	m.watcherInputs = newAutomationInputs([]string{"agent id", "path", "patterns (*.go,internal/*.go)", "workflow (optional)"})
	m.scheduleInputs = newAutomationInputs([]string{"agent id", "cron (* * * * *)", "at RFC3339 (optional)", "workflow (optional)"})
	m.privacyInput = newAutomationInput("custom regex")
}

func newAutomationInputs(placeholders []string) []textinput.Model {
	out := make([]textinput.Model, 0, len(placeholders))
	for _, placeholder := range placeholders {
		out = append(out, newAutomationInput(placeholder))
	}
	return out
}

func newAutomationInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = placeholder
	ti.CharLimit = 4096
	ti.Width = 58
	return ti
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func rowMarker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func toggleStatus(kind string, id int64, enabled bool) string {
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return state + " " + kind + " " + strconv.FormatInt(id, 10)
}
