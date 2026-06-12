package main

// Tools tab — PrAImate-managed companion CLIs. Browser
// shape mirrors the Agents picker, but each row is an installer.Tool
// instead of an Agent: detection probes PATH + the PrAImate-managed
// prefix, Enter / 'i' routes to the per-tool install screen, which
// streams uv / git output the same way the Agents install screen does.
//
// The Tools tab is intentionally separate from Agents because Tools
// are NOT launchable as a primary chat agent — they're callable
// helpers wpc-staged template scripts invoke. Putting them on the
// Agents picker would invite "Enter on graphify launches a chat" UX
// confusion.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// --- browser (Tools tab) -------------------------------------------------

type toolsModel struct {
	cfg     *launcher.Config
	items   []installer.Tool
	cursor  int
	loading bool
}

func newToolsBrowser(cfg *launcher.Config) toolsModel {
	return toolsModel{cfg: cfg, loading: true}
}

type toolsLoadedMsg struct{ items []installer.Tool }

func (m toolsModel) Init() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		return toolsLoadedMsg{items: installer.DetectTools(ctx)}
	}
}

func (m toolsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case toolsLoadedMsg:
		items := msg.items
		// Stable sort: available first, then by ID for determinism.
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Available != items[j].Available {
				return items[i].Available
			}
			return string(items[i].ID) < string(items[j].ID)
		})
		m.items = items
		m.loading = false
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, wrap(newChatListModel(m.cfg))
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter", "i":
			if m.cursor >= len(m.items) {
				return m, nil
			}
			return m, wrap(newToolInstallModel(m.cfg, m.items[m.cursor].ID))
		case "r":
			// Reload — useful right after install completes if the user
			// stays on the Tools tab and wants to see the version pick up.
			m.loading = true
			m.items = nil
			return m, m.Init()
		}
	}
	return m, nil
}

func (m toolsModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m toolsModel) Title() string {
	return "Tools · browse + install"
}

func (m toolsModel) Help() string {
	if len(m.items) == 0 {
		return "esc back"
	}
	return "↑/↓ select · enter or i install/update · r reload · esc back"
}

func (m toolsModel) NavSection() string   { return navSectionTools }
func (m toolsModel) CapturingInput() bool { return false }

func (m toolsModel) Body() string {
	var b strings.Builder
	if m.loading {
		b.WriteString(hintStyle.Render("Detecting tools..."))
		return b.String()
	}
	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No tools registered yet."))
		return b.String()
	}
	b.WriteString(hintStyle.Render(
		"Clade-managed companion CLIs. Tools are NOT launchable as a primary "+
			"agent — they are helpers wpc-staged template scripts invoke. "+
			"Install one here so workpaths that import its _common/ bundle "+
			"can call it on PATH.") + "\n\n")

	for i, t := range m.items {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		var status string
		switch {
		case t.Available && t.Version != "":
			status = availableStyle.Render(fmt.Sprintf("✓ %s", t.Version))
		case t.Available:
			status = availableStyle.Render("✓ installed")
		case t.ProbeError != "":
			status = errorStyle.Render("✗ probe failed")
		default:
			status = descStyle.Render("· not installed")
		}
		row := fmt.Sprintf("%s%-12s  %-46s  %s", marker, string(t.ID), t.Label, status)
		b.WriteString(selectionRow(row, isSel) + "\n")
		if isSel {
			if t.InstallHint != "" {
				b.WriteString(descStyle.Render("  hint: "+t.InstallHint) + "\n")
			}
			if t.Available {
				b.WriteString(descStyle.Render("  path: "+t.Binary) + "\n")
			}
			if t.ProbeError != "" {
				b.WriteString(descStyle.Render("  probe error: "+t.ProbeError) + "\n")
			}
		}
	}
	return b.String()
}

// --- per-tool install screen ---------------------------------------------

// toolInstallModel mirrors installModel (the agent installer) but for
// non-agent tools. Same runningOutput / tick pattern; different exit
// path (back to the Tools tab) and Title.
type toolInstallModel struct {
	cfg     *launcher.Config
	toolID  installer.ToolID
	methods []installer.Method
	cursor  int

	// running state
	running  bool
	output   *runningOutput
	exitErr  error
	exitDone bool
}

func newToolInstallModel(cfg *launcher.Config, id installer.ToolID) toolInstallModel {
	methods := installer.ToolMethods(id, installer.ActionInstall, installer.DetectOS())
	return toolInstallModel{
		cfg:     cfg,
		toolID:  id,
		methods: methods,
	}
}

func (m toolInstallModel) Init() tea.Cmd { return nil }

func (m toolInstallModel) exitTo() tea.Cmd {
	// Back to the Tools tab so the user sees the new status row
	// after a successful install.
	return wrap(newToolsBrowser(m.cfg))
}

func (m toolInstallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.running {
			if m.exitDone && (msg.String() == "esc" || msg.String() == "enter") {
				return m, m.exitTo()
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, m.exitTo()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.methods)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= len(m.methods) {
				return m, nil
			}
			chosen := m.methods[m.cursor]
			// For tools we don't surface the Node opt-in. Missing
			// prereqs (uv for graphify/scrapegraph; git+bun for gstack)
			// bubble up from installer.Run as a clear error message.
			m.running = true
			m.output = &runningOutput{}
			out := m.output
			return m, tea.Batch(
				func() tea.Msg {
					err := installer.Run(context.Background(), chosen, out, out)
					return installDoneMsg{err: err}
				},
				tickInstall(),
			)
		}
	case installDoneMsg:
		m.exitErr = msg.err
		m.exitDone = true
		return m, nil
	case installTickMsg:
		if !m.running || m.exitDone {
			return m, nil
		}
		return m, tickInstall()
	}
	return m, nil
}

func (m toolInstallModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m toolInstallModel) Title() string {
	return fmt.Sprintf("Install · %s", m.toolID)
}

func (m toolInstallModel) Help() string {
	if len(m.methods) == 0 {
		return "esc back"
	}
	if m.running {
		return "enter / esc to continue"
	}
	return "↑/↓ select · enter run · esc back"
}

func (m toolInstallModel) NavSection() string   { return navSectionTools }
func (m toolInstallModel) CapturingInput() bool { return false }

func (m toolInstallModel) Body() string {
	var b strings.Builder

	if len(m.methods) == 0 {
		b.WriteString(errorStyle.Render("✗ No install method available on this OS for "+string(m.toolID)) + "\n")
		b.WriteString(hintStyle.Render("Most common cause for tools: a missing prereq.") + "\n")
		b.WriteString(hintStyle.Render("Install uv for graphify/scrapegraph, or git + bun + bash for gstack.") + "\n")
		b.WriteString(descStyle.Render("  curl -LsSf https://astral.sh/uv/install.sh | sh    Linux/macOS") + "\n")
		b.WriteString(descStyle.Render("  irm https://astral.sh/uv/install.ps1 | iex         Windows") + "\n")
		return b.String()
	}

	if !m.running {
		b.WriteString(hintStyle.Render("Pick a method. Recommended is marked. "+
			"You'll see the exact command before it runs.") + "\n\n")
		for i, mth := range m.methods {
			isSel := i == m.cursor
			marker := "  "
			if isSel {
				marker = "› "
			}
			label := mth.Label
			if mth.Recommended {
				label += "   " + availableStyle.Render("[recommended]")
			}
			b.WriteString(selectionRow(marker+label, isSel) + "\n")
			b.WriteString(descStyle.Render("$ "+mth.Command) + "\n")
			if isSel && len(mth.Prereqs) > 0 {
				missing := installer.PrereqsMissing(mth)
				if len(missing) == 0 {
					b.WriteString(descStyle.Render("prereqs ok: "+strings.Join(mth.Prereqs, ", ")) + "\n")
				} else {
					b.WriteString(descStyle.Render(errorStyle.Render("you must install: "+strings.Join(missing, ", "))) + "\n")
				}
			}
			if isSel && mth.ManagedPrefix != "" {
				if prefix, err := installer.ManagedToolPrefix(mth.ManagedPrefix); err == nil {
					b.WriteString(descStyle.Render("  prefix: "+prefix) + "\n")
				}
			}
		}
		return b.String()
	}

	// Running / done view — tail only, same shape as the agent install
	// screen. Older lines scroll out so the final status + error stays
	// readable when the install emits a long progress wall.
	b.WriteString(hintStyle.Render("Running...") + "\n\n")
	lines := m.output.snapshot()
	start := 0
	if len(lines) > installOutputTailLines {
		start = len(lines) - installOutputTailLines
		b.WriteString(descStyle.Render(
			fmt.Sprintf("  … (%d earlier lines hidden) …\n", start)))
	}
	for _, l := range lines[start:] {
		b.WriteString("  " + l + "\n")
	}
	if m.exitDone {
		b.WriteString("\n")
		if m.exitErr != nil {
			b.WriteString(errorStyle.Render("✗ Failed: " + m.exitErr.Error()))
		} else {
			b.WriteString(okStyle.Render("✓ Done — press esc / enter to return to the Tools tab"))
		}
	}
	return b.String()
}
