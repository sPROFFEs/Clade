package main

// Workpath file editor screen. Lists the editable files in a workpath
// (mission/playbook/rules) plus a "Open workpath dir" escape hatch for
// browsing tools/, agents/, etc. Pressing Enter on a file suspends
// Bubble Tea and opens the file in the user's $EDITOR (sane fallbacks
// per OS); when the editor exits, the launcher resumes on this screen.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

type fileEntry struct {
	label string
	path  string // relative to workpath dir
	hint  string
}

type filesModel struct {
	cfg         *launcher.Config
	workpathDir string
	label       string // human-friendly subject ("template foo", "chat bar")
	parent      tea.Model

	entries []fileEntry
	cursor  int
	last    string // status line: last action's result
	err     string
}

// newFilesModel takes the directory holding mission.md etc. plus a
// caller-supplied "parent" model to return to on esc.
func newFilesModel(cfg *launcher.Config, workpathDir, label string, parent tea.Model) filesModel {
	return filesModel{
		cfg:         cfg,
		workpathDir: workpathDir,
		label:       label,
		parent:      parent,
		entries: []fileEntry{
			{label: "mission.md", path: "mission.md", hint: "What this workpath is for + output shape"},
			{label: "playbook.md", path: "playbook.md", hint: "Staged process the agent follows"},
			{label: "rules.md", path: "rules.md", hint: "Hard constraints / never-dos / always-dos"},
			{label: "Open workpath/ dir in file manager", path: "", hint: "Browse + edit tools/, agents/, anything else"},
		},
	}
}

func (m filesModel) Init() tea.Cmd { return nil }

// editorExecFinishedMsg is the result of tea.ExecProcess. err is nil on
// clean exit (or no editor configured but a fallback succeeded).
type editorExecFinishedMsg struct {
	target string
	err    error
}

func (m filesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editorExecFinishedMsg:
		if msg.err != nil {
			m.err = fmt.Sprintf("editor for %s: %v", msg.target, msg.err)
			m.last = ""
		} else {
			m.last = "saved: " + msg.target
			m.err = ""
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, wrap(m.parent)
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "enter":
			e := m.entries[m.cursor]
			if e.path == "" {
				if err := openInFileManager(m.workpathDir); err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.last = "opened " + m.workpathDir + " in file manager"
				m.err = ""
				return m, nil
			}
			full := filepath.Join(m.workpathDir, e.path)
			// Ensure the file exists so the editor doesn't open a blank
			// buffer with a confusing "new file" indicator.
			if _, err := os.Stat(full); err != nil {
				if writeErr := os.WriteFile(full, []byte("# "+e.path+"\n"), 0o644); writeErr != nil {
					m.err = "create " + e.path + ": " + writeErr.Error()
					return m, nil
				}
			}
			editor, args := editorCommand()
			if editor == "" {
				m.err = "no editor found (set $EDITOR or install nano/vi/notepad)"
				return m, nil
			}
			c := exec.Command(editor, append(args, full)...)
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return editorExecFinishedMsg{target: e.path, err: err}
			})
		}
	}
	return m, nil
}

func (m filesModel) View() string {
	var b strings.Builder
	title := fmt.Sprintf("Edit files · %s", m.label)
	help := "↑/↓ select · enter edit · esc back"

	b.WriteString(subtitleStyle.Render("Workpath: ") + m.workpathDir + "\n\n")
	for i, e := range m.entries {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		b.WriteString(selectionRow(marker+e.label, isSel) + "\n")
		if isSel && e.hint != "" {
			b.WriteString(descStyle.Render(e.hint) + "\n")
		}
	}
	b.WriteString("\n")
	if m.last != "" {
		b.WriteString(okStyle.Render("✓ "+m.last) + "\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n")
	}
	editor, _ := editorCommand()
	b.WriteString(hintStyle.Render("Editor: " + editor))
	return renderChrome(title, b.String(), help)
}

// editorCommand returns the editor to spawn and any leading args.
// Order: $VISUAL → $EDITOR → OS-appropriate fallback.
func editorCommand() (string, []string) {
	if v := strings.TrimSpace(os.Getenv("VISUAL")); v != "" {
		parts := strings.Fields(v)
		return parts[0], parts[1:]
	}
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		parts := strings.Fields(v)
		return parts[0], parts[1:]
	}
	switch runtime.GOOS {
	case "windows":
		// notepad ships with every Windows install.
		return "notepad", nil
	default:
		// Try a chain of common Unix editors.
		for _, ed := range []string{"nano", "vim", "vi"} {
			if _, err := exec.LookPath(ed); err == nil {
				return ed, nil
			}
		}
	}
	return "", nil
}

// openInFileManager opens the given directory in the OS file manager.
// Returns an error if no appropriate command is available.
func openInFileManager(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s %s: %w", cmd.Path, dir, err)
	}
	// Don't wait — file manager runs in the background.
	go func() { _ = cmd.Wait() }()
	return nil
}
