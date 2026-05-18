package main

// Workspace settings editor. Reuses the same fields as the create wizard
// but lets the user revisit them on an existing workspace. Stored to
// <workspace>/workspace.json on save.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

type settingsModel struct {
	cfg *launcher.Config
	ws  launcher.Workspace

	language   textinput.Model
	skillInput textinput.Model
	memory     bool
	skills     []string

	step int    // 0: language, 1: memory, 2: skills, 3: done
	err  string
	ok   string
}

func newSettingsModel(cfg *launcher.Config, ws launcher.Workspace) settingsModel {
	lang := textinput.New()
	lang.Placeholder = "blank to clear"
	lang.Width = 40
	lang.CharLimit = 60
	lang.SetValue(ws.Settings.Language)
	lang.Focus()

	skill := textinput.New()
	skill.Placeholder = "git URL — blank Enter to finish"
	skill.Width = 70
	skill.CharLimit = 300

	return settingsModel{
		cfg:        cfg,
		ws:         ws,
		language:   lang,
		skillInput: skill,
		memory:     ws.Settings.MemoryEnabled,
		skills:     append([]string(nil), ws.Settings.OnlineSkills...),
	}
}

func (m settingsModel) Init() tea.Cmd { return textinput.Blink }

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.step == 1 {
			switch msg.String() {
			case "y", "Y":
				m.memory = true
				m.step = 2
				m.skillInput.Focus()
				return m, textinput.Blink
			case "n", "N":
				m.memory = false
				m.step = 2
				m.skillInput.Focus()
				return m, textinput.Blink
			case " ":
				m.memory = !m.memory
				return m, nil
			case "enter":
				m.step = 2
				m.skillInput.Focus()
				return m, textinput.Blink
			case "esc":
				m.step = 0
				m.language.Focus()
				return m, textinput.Blink
			}
			return m, nil
		}

		if m.step == 3 {
			if msg.String() == "esc" || msg.String() == "enter" {
				return m, wrap(newChatListModel(m.cfg))
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyEsc:
			if m.step == 0 {
				return m, wrap(newChatListModel(m.cfg))
			}
			m.step--
			return m, nil
		case tea.KeyEnter:
			switch m.step {
			case 0:
				m.step = 1
				m.language.Blur()
				return m, nil
			case 2:
				url := strings.TrimSpace(m.skillInput.Value())
				if url != "" {
					m.skills = append(m.skills, url)
					m.skillInput.SetValue("")
					return m, nil
				}
				// Save and finish.
				m.ws.Settings.Language = strings.TrimSpace(m.language.Value())
				m.ws.Settings.MemoryEnabled = m.memory
				m.ws.Settings.OnlineSkills = m.skills
				if err := launcher.SaveWorkspaceLikeSettings(m.ws); err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.ok = "saved to " + m.ws.Root + "/workspace.json"
				m.step = 3
				return m, nil
			}
		}

		// Single-key shortcut on the skills step: 'd' to delete the last URL.
		if m.step == 2 && msg.String() == "ctrl+d" && len(m.skills) > 0 {
			m.skills = m.skills[:len(m.skills)-1]
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.step {
	case 0:
		m.language, cmd = m.language.Update(msg)
	case 2:
		m.skillInput, cmd = m.skillInput.Update(msg)
	}
	return m, cmd
}

func (m settingsModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m settingsModel) Title() string {
	return fmt.Sprintf("Settings · %s", m.ws.Name)
}
func (m settingsModel) Help() string       { return "enter to continue · esc to go back" }
func (m settingsModel) NavSection() string { return navSectionChats }

func (m settingsModel) Body() string {
	var b strings.Builder

	switch m.step {
	case 0:
		b.WriteString(inputLabelStyle.Render("Default language: "))
		b.WriteString(m.language.View() + "\n")
		b.WriteString(descStyle.Render("Added as 'respond in <lang>' directive at launch.") + "\n")
	case 1:
		mark := "[ ] no"
		if m.memory {
			mark = availableStyle.Render("[x] yes")
		}
		b.WriteString(inputLabelStyle.Render("Persistent MEMORY.md? ") + mark + "\n")
		b.WriteString(descStyle.Render("y/n · space to toggle · enter to accept current") + "\n")
	case 2:
		b.WriteString(inputLabelStyle.Render("Online skill URL: "))
		b.WriteString(m.skillInput.View() + "\n")
		b.WriteString(descStyle.Render("Enter URL or blank Enter to save · ctrl-d removes last entry") + "\n")
		for i, u := range m.skills {
			b.WriteString(descStyle.Render(fmt.Sprintf("  %d. %s", i+1, u)) + "\n")
		}
	case 3:
		b.WriteString(okStyle.Render("✓ "+m.ok) + "\n")
	}

	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return b.String()
}
