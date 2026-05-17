package main

// Template management: list / new / delete. Editing a template's
// settings (language, memory toggle, etc.) reuses the existing settings
// screen by passing the template via its AsWorkspace bridge.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

type templateListModel struct {
	cfg       *launcher.Config
	items     []launcher.Template
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
		items, err := launcher.ListTemplates(cfg.WorkspacesRoot)
		return templatesLoadedMsg{items: items, err: err}
	}
}

func (m templateListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case templatesLoadedMsg:
		m.loaded = true
		m.items = msg.items
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
		case "r":
			return m, m.Init()
		}
	}
	return m, nil
}

func (m templateListModel) View() string {
	var b strings.Builder
	b.WriteString(header("Templates · reusable patterns for chats"))
	b.WriteString("\n")
	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading templates...") + "\n")
		return b.String()
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("Error: "+m.err) + "\n\n")
	}
	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No templates yet — create one to start cloning chats from it.") + "\n\n")
	}
	for i, t := range m.items {
		marker := "  "
		r := listItemStyle.Render
		if i == m.cursor {
			marker = "› "
			r = listItemSelectedStyle.Render
		}
		b.WriteString(r(marker+t.Name) + "\n")
		if i == m.cursor && t.Description != "" {
			b.WriteString(descStyle.Render(t.Description) + "\n")
		}
	}
	marker := "  "
	r := listItemStyle.Render
	if m.cursor == len(m.items) {
		marker = "› "
		r = listItemSelectedStyle.Render
	}
	b.WriteString(r(marker+"+ new template…") + "\n")

	if m.deleteAsk && m.cursor < len(m.items) {
		b.WriteString("\n" + errorStyle.Render(
			fmt.Sprintf("Delete template %q? Existing chats cloned from it are unaffected. (y/n)",
				m.items[m.cursor].Name)) + "\n")
	}

	b.WriteString(helpStyle.Render(
		"↑/↓ select · enter edit settings · n new · d delete · r refresh · esc back"))
	return b.String()
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
	var b strings.Builder
	b.WriteString(header(fmt.Sprintf("New template (step %d/5)", m.step+1)))
	b.WriteString("\n")
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
		b.WriteString("\n" + errorStyle.Render("Error: "+m.err))
	}
	b.WriteString(helpStyle.Render("enter to continue · esc to go back · ctrl-c to abort"))
	return b.String()
}
