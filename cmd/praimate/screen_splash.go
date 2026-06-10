package main

// Boot splash: a brief animated logo that runs when `praimate`
// starts, then transitions to whatever the real first screen is
// (first-run wizard or chat list). Codex-CLI-style draw-in: the logo
// reveals column-by-column from left to right, the wordmark pulses,
// then we hand off.
//
// Skip with --no-splash, or set PRAIMATE_NO_SPLASH=1 in the env.
// Skipped automatically when stdin/stdout isn't a TTY (CI, piped),
// because there's nothing to look at.

import (
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The praimate wordmark with the monkey mascot inlined on each row.
// Six lines total: ASCII monkey on the left, figlet "ANSI Shadow"
// PRAIMATE letters on the right. We reveal characters column-by-
// column to produce the draw-in effect.
//
// If you regenerate, keep all rows the same rune-width or the
// reveal will look uneven (the reveal pads each frame to totalCols
// so the centred layout stays stable across frames).
var logoWordmark = []string{
	"   .-\"-.     ██████╗ ██████╗  █████╗ ██╗███╗   ███╗ █████╗ ████████╗███████╗",
	"  /|6 6|\\    ██╔══██╗██╔══██╗██╔══██╗██║████╗ ████║██╔══██╗╚══██╔══╝██╔════╝",
	" {/(_0_)\\}   ██████╔╝██████╔╝███████║██║██╔████╔██║███████║   ██║   █████╗  ",
	"  _/ ^ \\_    ██╔═══╝ ██╔══██╗██╔══██║██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝  ",
	" (/ /^\\ \\)   ██║     ██║  ██║██║  ██║██║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗",
	"  \"\"' '\"\"    ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝",
}

// Pre-computed wordmark column count, used to drive the reveal.
func wordmarkCols() int {
	max := 0
	for _, r := range logoWordmark {
		// Count runes, not bytes — these rows contain multibyte
		// box-drawing characters.
		n := len([]rune(r))
		if n > max {
			max = n
		}
	}
	return max
}

// Skip-splash detection. We honour:
//
//	--no-splash command-line flag (set by main.go's flag.Parse)
//	PRAIMATE_NO_SPLASH=1 env var
//	non-interactive stdout (piped, redirected, CI)
//
// The flag is parsed before main() builds the model, so we read it
// here from a package-level var that main writes to.
var noSplashFlag bool

func splashEnabled() bool {
	if noSplashFlag {
		return false
	}
	if os.Getenv("PRAIMATE_NO_SPLASH") == "1" {
		return false
	}
	// If stdout isn't a terminal there's no point animating.
	if fi, err := os.Stdout.Stat(); err == nil {
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			return false
		}
	}
	return true
}

type splashModel struct {
	next      tea.Model // screen to transition to after the splash
	cols      int       // how many columns of the wordmark are revealed
	totalCols int
	phase     splashPhase
	pulse     int // counter for the wordmark colour cycle in the hold phase
}

type splashPhase int

const (
	splashRevealing splashPhase = iota // characters drawing in
	splashHolding                      // fully drawn, wordmark pulsing
	splashDone                         // ready to transition
)

func newSplashModel(next tea.Model) splashModel {
	return splashModel{
		next:      next,
		totalCols: wordmarkCols(),
		phase:     splashRevealing,
	}
}

// Tick cadences. revealStep is fast (the eye reads ~40 cps in this
// kind of animation); the hold is long enough to register the
// finished logo without delaying the boot path past ~1.5s total.
const (
	splashRevealStep = 18 * time.Millisecond
	splashPulseStep  = 90 * time.Millisecond
	splashHoldFrames = 6 // pulse frames before transitioning
)

type splashTickMsg struct{}

func splashTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return splashTickMsg{} })
}

func (m splashModel) Init() tea.Cmd {
	return splashTick(splashRevealStep)
}

func (m splashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		// Any key skips the splash — same convention as Codex/lazygit.
		return m, wrap(m.next)
	case splashTickMsg:
		switch m.phase {
		case splashRevealing:
			m.cols += 2 // 2 cols/tick → ~40 cols at 18ms ≈ 360ms reveal
			if m.cols >= m.totalCols {
				m.cols = m.totalCols
				m.phase = splashHolding
				m.pulse = 0
				return m, splashTick(splashPulseStep)
			}
			return m, splashTick(splashRevealStep)
		case splashHolding:
			m.pulse++
			if m.pulse >= splashHoldFrames {
				m.phase = splashDone
				return m, wrap(m.next)
			}
			return m, splashTick(splashPulseStep)
		}
	}
	return m, nil
}

func (m splashModel) View() string {
	var b strings.Builder

	// Top padding so the logo sits roughly centred vertically in a
	// typical 24-30 row terminal. bodyHeight isn't available so we
	// just push down a few lines.
	b.WriteString("\n\n\n")

	// Wordmark: reveal m.cols columns of each row from the left,
	// padding the rest with spaces so trailing-row geometry stays
	// stable (otherwise the centre() helper rejiggers each frame).
	// During holding we cycle the wordmark's colour between Accent and
	// Title (cyan / magenta in the default theme) to fake a "pulse"
	// glow without doing any colour math.
	wordStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if m.phase == splashHolding && m.pulse%2 == 1 {
		wordStyle = lipgloss.NewStyle().Foreground(t.Title).Bold(true)
	}
	for _, row := range logoWordmark {
		runes := []rune(row)
		var slice []rune
		if m.cols >= len(runes) {
			slice = runes
		} else {
			slice = runes[:m.cols]
		}
		shown := string(slice)
		// Pad to total width so each frame has the same on-screen
		// length and centre() centres on the FULL wordmark.
		pad := m.totalCols - len([]rune(shown))
		if pad > 0 {
			shown += strings.Repeat(" ", pad)
		}
		b.WriteString(centre(wordStyle.Render(shown)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	tagStyle := lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
	tagline := "one harness, every agent — shared memory & MCP"
	if m.phase == splashRevealing {
		// Only show the tagline once the wordmark finishes drawing.
		tagline = ""
	}
	b.WriteString(centre(tagStyle.Render(tagline)))
	b.WriteString("\n\n")

	hint := lipgloss.NewStyle().Foreground(t.Muted).Render("press any key to skip")
	b.WriteString(centre(hint))

	return b.String()
}

// centre pads a single line of text with leading spaces so its
// visible content sits centred in the current terminal width. The
// input may already contain ANSI escapes — we measure with lipgloss
// so they don't count toward the visible width.
func centre(line string) string {
	w := termWidth
	if w <= 0 {
		w = 100
	}
	visible := lipgloss.Width(line)
	pad := (w - visible) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + line
}
