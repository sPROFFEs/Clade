package main

// Template management: list / new / delete. Editing a template's
// settings (language, memory toggle, etc.) reuses the existing settings
// screen by passing the template via its AsWorkspace bridge.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

type templateListModel struct {
	cfg       *launcher.Config
	items     []launcher.Template
	skipped   []launcher.SkippedTemplate
	cursor    int
	loaded    bool
	err       string
	deleteAsk bool
}

func newTemplateListModel(cfg *launcher.Config) templateListModel {
	return templateListModel{cfg: cfg}
}

func (m templateListModel) Init() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		items, skipped, err := launcher.ListTemplatesAndSkipped(cfg.WorkspacesRoot)
		return templatesLoadedMsg{items: items, skipped: skipped, err: err}
	}
}

func (m templateListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case templatesLoadedMsg:
		m.loaded = true
		m.items = msg.items
		m.skipped = msg.skipped
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		if m.deleteAsk {
			switch msg.String() {
			case "y", "Y":
				if m.cursor < len(m.items) {
					_ = launcher.DeleteTemplate(m.cfg.WorkspacesRoot, m.items[m.cursor].Name)
				}
				m.deleteAsk = false
				return m, m.Init()
			case "n", "N", "esc":
				m.deleteAsk = false
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, wrap(newChatListModel(m.cfg))
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items) {
				m.cursor++
			}
		case "enter":
			if m.cursor == len(m.items) {
				return m, wrap(newNewTemplateModel(m.cfg))
			}
			// Edit settings of the highlighted template.
			t := m.items[m.cursor]
			ws := launcher.Workspace{
				Name: t.Name, Root: t.Root, WorkpathDir: t.WorkpathDir,
				Description: t.Description, Settings: t.Settings,
			}
			return m, wrap(newSettingsModel(m.cfg, ws))
		case "n":
			return m, wrap(newNewTemplateModel(m.cfg))
		case "d":
			if m.cursor < len(m.items) {
				m.deleteAsk = true
			}
		case "f":
			// Edit template workpath files (mission/playbook/rules etc.).
			if m.cursor < len(m.items) {
				t := m.items[m.cursor]
				parent := newTemplateListModel(m.cfg)
				return m, wrap(newFilesModel(m.cfg, t.WorkpathDir, "template "+t.Name, parent))
			}
		case "r":
			return m, m.Init()
		}
	}
	return m, nil
}

func (m templateListModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m templateListModel) Title() string {
	return fmt.Sprintf("Templates (%d) · reusable patterns for chats", len(m.items))
}
func (m templateListModel) Help() string          { return templateListHelp(m) }
func (m templateListModel) NavSection() string    { return navSectionTemplates }
func (m templateListModel) CapturingInput() bool  { return false }

func (m templateListModel) Body() string {
	var b strings.Builder

	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading templates..."))
		return b.String()
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n\n")
	}
	// Surface dirs we had to skip (typically: user dropped files into
	// <root>/templates/foo/ without nesting them inside workpath/, or
	// without a mission.md at all).
	if len(m.skipped) > 0 {
		b.WriteString(errorStyle.Render(fmt.Sprintf(
			"Skipped %d dir(s) under templates/ — fix or remove them:", len(m.skipped))) + "\n")
		for _, s := range m.skipped {
			b.WriteString(descStyle.Render(fmt.Sprintf("  • %s — %s", s.Name, s.Reason)) + "\n")
		}
		b.WriteString("\n")
	}
	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No templates yet — press n to create one, or drop a workpath dir under templates/.") + "\n\n")
	}
	for i, tpl := range m.items {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		b.WriteString(selectionRow(marker+tpl.Name, isSel) + "\n")
		if isSel && tpl.Description != "" {
			b.WriteString(descStyle.Render(tpl.Description) + "\n")
		}
	}
	isSelNew := m.cursor == len(m.items)
	marker := "  "
	if isSelNew {
		marker = "› "
	}
	b.WriteString(selectionRow(marker+"+ new template…", isSelNew) + "\n")

	if m.deleteAsk && m.cursor < len(m.items) {
		b.WriteString("\n" + errorStyle.Render(
			fmt.Sprintf("Delete template %q? Existing chats cloned from it are unaffected. (y/n)",
				m.items[m.cursor].Name)) + "\n")
	}

	return b.String()
}

// templateListHelp omits the per-template keys when no template is
// highlighted, so users on an empty list aren't told about d/f/enter.
func templateListHelp(m templateListModel) string {
	parts := []string{"↑/↓ select"}
	if m.cursor < len(m.items) {
		parts = append(parts, "enter edit settings", "f edit files", "d delete")
	} else if len(m.items) == 0 {
		// nothing — just "+ new template" is selectable; Enter is enough.
	} else {
		parts = append(parts, "enter new template")
	}
	parts = append(parts, "n new", "r refresh", "esc back")
	return strings.Join(parts, " · ")
}

// --- new template: the old NewWorkspace wizard, lightly renamed ---------

type newTemplateModel struct {
	cfg         *launcher.Config
	name        textinput.Model
	description textinput.Model
	language    textinput.Model
	skillInput  textinput.Model
	memory      bool
	skills      []string
	step        int
	err         string
}

func newNewTemplateModel(cfg *launcher.Config) newTemplateModel {
	mk := func(ph string, w, lim int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = lim
		ti.Width = w
		return ti
	}
	name := mk("kebab-case (a-z, 0-9, '-' '_')", 50, 64)
	name.Focus()
	desc := mk("one-line summary of what this template's for", 60, 200)
	lang := mk("blank to skip · e.g. 'es', 'ja', 'Italian'", 40, 60)
	skill := mk("git URL — blank Enter to finish", 70, 300)
	return newTemplateModel{cfg: cfg, name: name, description: desc, language: lang, skillInput: skill}
}

func (m newTemplateModel) Init() tea.Cmd { return textinput.Blink }

func (m newTemplateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.step == 3 {
			switch msg.String() {
			case "y", "Y":
				m.memory = true
				m.step = 4
				m.skillInput.Focus()
				return m, textinput.Blink
			case "n", "N":
				m.memory = false
				m.step = 4
				m.skillInput.Focus()
				return m, textinput.Blink
			case " ":
				m.memory = !m.memory
				return m, nil
			case "enter":
				m.step = 4
				m.skillInput.Focus()
				return m, textinput.Blink
			case "esc":
				m.step = 2
				m.language.Focus()
				return m, textinput.Blink
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			if m.step == 0 {
				return m, wrap(newTemplateListModel(m.cfg))
			}
			m.step--
			return m, nil
		case tea.KeyEnter:
			switch m.step {
			case 0:
				name := strings.TrimSpace(m.name.Value())
				if err := launcher.ValidateName(name); err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.err = ""
				m.name.SetValue(name)
				m.step = 1
				m.name.Blur()
				m.description.Focus()
				return m, textinput.Blink
			case 1:
				m.step = 2
				m.description.Blur()
				m.language.Focus()
				return m, textinput.Blink
			case 2:
				m.step = 3
				m.language.Blur()
				return m, nil
			case 4:
				url := strings.TrimSpace(m.skillInput.Value())
				if url != "" {
					m.skills = append(m.skills, url)
					m.skillInput.SetValue("")
					return m, nil
				}
				m.step = 5
				cfg := m.cfg
				name := strings.TrimSpace(m.name.Value())
				desc := strings.TrimSpace(m.description.Value())
				lang := strings.TrimSpace(m.language.Value())
				mem := m.memory
				urls := append([]string(nil), m.skills...)
				return m, func() tea.Msg {
					tpl, err := launcher.CreateTemplate(cfg.WorkspacesRoot, name, desc)
					if err != nil {
						return errMsg{err: err}
					}
					tpl.Settings.Language = lang
					tpl.Settings.MemoryEnabled = mem
					tpl.Settings.OnlineSkills = urls
					if err := launcher.SaveTemplateSettings(tpl); err != nil {
						return errMsg{err: err}
					}
					return screenDoneMsg{next: newTemplateListModel(cfg)}
				}
			}
		}
	case errMsg:
		m.step = 0
		m.err = msg.err.Error()
		m.name.Focus()
		return m, nil
	}
	var cmd tea.Cmd
	switch m.step {
	case 0:
		m.name, cmd = m.name.Update(msg)
		// Names must be lowercase (see launcher.ValidateName). Normalising
		// as the user types means uppercase keystrokes silently become the
		// lowercase letter instead of triggering a validation error at
		// submit time.
		if v := m.name.Value(); v != strings.ToLower(v) {
			pos := m.name.Position()
			m.name.SetValue(strings.ToLower(v))
			m.name.SetCursor(pos)
		}
	case 1:
		m.description, cmd = m.description.Update(msg)
	case 2:
		m.language, cmd = m.language.Update(msg)
	case 4:
		m.skillInput, cmd = m.skillInput.Update(msg)
	}
	return m, cmd
}

func (m newTemplateModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m newTemplateModel) Title() string {
	return fmt.Sprintf("New template (step %d/5)", m.step+1)
}
func (m newTemplateModel) Help() string          { return "enter to continue · esc to go back" }
func (m newTemplateModel) NavSection() string    { return navSectionTemplates }
// All five steps either type into a textinput (name/desc/language/skill)
// or are y/n toggles — both want `:` and `?` to flow through as-is.
func (m newTemplateModel) CapturingInput() bool  { return true }

func (m newTemplateModel) Body() string {
	var b strings.Builder
	b.WriteString(hintStyle.Render("Name + description are required. Language / memory / online skills are optional defaults — each chat created from this template inherits them at creation."))
	b.WriteString("\n\n")
	if m.step >= 1 {
		b.WriteString(subtitleStyle.Render("Name: ") + m.name.Value() + "\n")
	}
	if m.step >= 2 {
		b.WriteString(subtitleStyle.Render("Description: ") + m.description.Value() + "\n")
	}
	if m.step >= 3 && m.language.Value() != "" {
		b.WriteString(subtitleStyle.Render("Language: ") + m.language.Value() + "\n")
	}
	if m.step >= 4 {
		mark := "no"
		if m.memory {
			mark = availableStyle.Render("yes")
		}
		b.WriteString(subtitleStyle.Render("Memory: ") + mark + "\n")
	}
	if m.step >= 4 && len(m.skills) > 0 {
		b.WriteString(subtitleStyle.Render("Online skills: ") + fmt.Sprintf("%d added\n", len(m.skills)))
	}
	if m.step >= 1 {
		b.WriteString("\n")
	}
	switch m.step {
	case 0:
		b.WriteString(inputLabelStyle.Render("Name: "))
		b.WriteString(m.name.View() + "\n")
	case 1:
		b.WriteString(inputLabelStyle.Render("Description: "))
		b.WriteString(m.description.View() + "\n")
	case 2:
		b.WriteString(inputLabelStyle.Render("Default language (optional): "))
		b.WriteString(m.language.View() + "\n")
		b.WriteString(descStyle.Render("Adds a 'respond in <lang>' directive at launch.") + "\n")
	case 3:
		mark := "[ ] no"
		if m.memory {
			mark = availableStyle.Render("[x] yes")
		}
		b.WriteString(inputLabelStyle.Render("Persistent MEMORY.md? ") + mark + "\n")
		b.WriteString(descStyle.Render("y/n · space to toggle · enter to accept current") + "\n")
	case 4:
		b.WriteString(inputLabelStyle.Render("Online skill URL: "))
		b.WriteString(m.skillInput.View() + "\n")
		b.WriteString(descStyle.Render("Enter URL or blank Enter to finish.") + "\n")
		for i, u := range m.skills {
			b.WriteString(descStyle.Render(fmt.Sprintf("  %d. %s", i+1, u)) + "\n")
		}
	case 5:
		b.WriteString(hintStyle.Render("Scaffolding template...") + "\n")
	}
	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return b.String()
}
