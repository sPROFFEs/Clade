package targets

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdksdk/wpc/pkg/workpath"
)

// copyFile copies src → dst, creating dst's parent dir. Preserves the source
// file's mode bits so executable scripts stay executable.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// writeFile writes content to dst, creating parent dirs.
func writeFile(dst, content string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(content), 0o644)
}

// readFile is os.ReadFile that returns a string.
func readFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(raw), nil
}

// renderMissionBlock produces the markdown body shared by most targets:
// mission + optional playbook + optional rules. Each body is included
// verbatim if it already starts with a heading; otherwise it's wrapped in a
// generated H2 section header. This keeps hand-authored mission.md files
// with rich H1 structure intact while still giving plain-prose bodies a
// section title.
func renderMissionBlock(wp *workpath.Workpath) string {
	var b strings.Builder
	b.WriteString(section("Mission", wp.Mission))
	if wp.Playbook != "" {
		b.WriteString("\n")
		b.WriteString(section("Playbook", wp.Playbook))
	}
	if wp.Rules != "" {
		b.WriteString("\n")
		b.WriteString(section("Rules", wp.Rules))
	}
	return b.String()
}

func section(title, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if strings.HasPrefix(body, "#") {
		return body + "\n"
	}
	return "## " + title + "\n\n" + body + "\n"
}

// renderToolList returns a markdown bullet list of tools, or empty string if
// there are none.
func renderToolList(wp *workpath.Workpath) string {
	if len(wp.Tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Tools\n\n")
	for _, t := range wp.Tools {
		desc := t.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- `%s` — %s (`%s`)\n", t.Name, desc, t.Script)
	}
	return b.String()
}

// renderAgentList returns a markdown bullet list of agents.
func renderAgentList(wp *workpath.Workpath) string {
	if len(wp.Agents) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Subagents\n\n")
	for _, a := range wp.Agents {
		desc := a.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", a.Name, desc)
		if len(a.Tools) > 0 {
			fmt.Fprintf(&b, "  - tools: %s\n", strings.Join(a.Tools, ", "))
		}
	}
	return b.String()
}
