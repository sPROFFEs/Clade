package workpath

import (
	"fmt"
	"regexp"
	"strings"
)

// nameRE enforces a conservative identifier shape: lowercase letters, digits,
// hyphens, underscores. Targets that have stricter rules (e.g. Claude Code
// requires kebab-case skill names) sanitize further at compile time.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Validate checks a loaded Workpath for fatal problems. Returns nil if the
// workpath is fit to compile to any target.
func Validate(wp *Workpath) error {
	if wp == nil {
		return fmt.Errorf("workpath is nil")
	}
	var issues []string

	if !nameRE.MatchString(wp.Name) {
		issues = append(issues, fmt.Sprintf("name %q must match %s", wp.Name, nameRE))
	}
	if strings.TrimSpace(wp.Description) == "" {
		issues = append(issues, "description is required (set in workpath.json or first line of mission.md)")
	}
	if strings.TrimSpace(wp.Mission) == "" {
		issues = append(issues, "mission.md is required and must be non-empty")
	}

	toolNames := map[string]bool{}
	for i, t := range wp.Tools {
		if !nameRE.MatchString(t.Name) {
			issues = append(issues, fmt.Sprintf("tools[%d] name %q must match %s", i, t.Name, nameRE))
		}
		if toolNames[t.Name] {
			issues = append(issues, fmt.Sprintf("tools[%d] name %q is duplicated", i, t.Name))
		}
		toolNames[t.Name] = true
		if t.Script == "" {
			issues = append(issues, fmt.Sprintf("tools[%d] (%s) has empty script path", i, t.Name))
		}
	}

	agentNames := map[string]bool{}
	for i, a := range wp.Agents {
		if !nameRE.MatchString(a.Name) {
			issues = append(issues, fmt.Sprintf("agents[%d] name %q must match %s", i, a.Name, nameRE))
		}
		if agentNames[a.Name] {
			issues = append(issues, fmt.Sprintf("agents[%d] name %q is duplicated", i, a.Name))
		}
		agentNames[a.Name] = true
		if a.Prompt == "" {
			issues = append(issues, fmt.Sprintf("agents[%d] (%s) has empty prompt path", i, a.Name))
		}
		for _, tn := range a.Tools {
			if !toolNames[tn] {
				issues = append(issues, fmt.Sprintf("agents[%d] (%s) references unknown tool %q", i, a.Name, tn))
			}
		}
	}

	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("invalid workpath:\n  - %s", strings.Join(issues, "\n  - "))
}
