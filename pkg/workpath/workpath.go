// Package workpath defines the in-memory representation of a workpath
// (also called a skill-path): a self-contained bundle describing a mission,
// staged playbook, hard rules, executable tools, and named subagents.
//
// The source-on-disk format is a directory:
//
//	<name>/
//	  workpath.json   (optional metadata: description, version, tool/agent overrides)
//	  mission.md      (required: what this workpath is for)
//	  playbook.md     (optional: staged process)
//	  rules.md        (optional: hard constraints)
//	  tools/*.sh|*.ps1 (optional: executables, auto-registered)
//	  agents/*.md     (optional: subagent prompts)
//
// The Workpath struct is target-agnostic. Per-target compilation lives in
// pkg/targets.
package workpath

// Workpath is the parsed, validated representation of a workpath directory.
type Workpath struct {
	// Name is the workpath identifier (derived from the source directory name
	// unless overridden in workpath.json).
	Name string

	// Description is a one-line summary used by every target's discovery
	// surface (Claude Code skill frontmatter, mika module header, Cursor
	// rule frontmatter, etc.). Required.
	Description string

	// Version defaults to "1" if unset.
	Version string

	// License is optional metadata, passed through to targets that support it.
	License string

	// Mission is the body of mission.md (required, non-empty).
	Mission string

	// Playbook is the body of playbook.md (optional, may be empty).
	Playbook string

	// Rules is the body of rules.md (optional, may be empty).
	Rules string

	// Tools are executable scripts the agent can invoke. Auto-discovered from
	// tools/*.sh and tools/*.ps1 unless overridden in workpath.json.
	Tools []Tool

	// Agents are subagent prompts with their own (optional) tool allowlist.
	// Auto-discovered from agents/*.md unless overridden.
	Agents []Agent

	// SourceDir is the absolute path the workpath was loaded from. Targets
	// use this to resolve relative script/agent paths.
	SourceDir string
}

// Tool is a shell script registered as an agent-callable command.
type Tool struct {
	Name        string // identifier (e.g. "file_summary")
	Description string // single line, surfaced to the agent
	Script      string // path relative to SourceDir (e.g. "tools/file_summary.sh")
	Shell       string // "bash" for .sh, "pwsh" for .ps1; inferred from extension
}

// Agent is a named subagent prompt with an optional tool allowlist.
type Agent struct {
	Name        string   // identifier (e.g. "triage")
	Description string   // single line, surfaced when the parent agent picks a subagent
	Prompt      string   // path relative to SourceDir (e.g. "agents/triage.md")
	Tools       []string // tool names this subagent may use; empty = all
}

// Manifest is the on-disk shape of workpath.json. All fields are optional;
// see workpath_test.go for examples.
type Manifest struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	License     string         `json:"license,omitempty"`
	Tools       []ToolOverride `json:"tools,omitempty"`
	Agents      []AgentOverride `json:"agents,omitempty"`
}

// ToolOverride lets the manifest pin a tool's name/description/script
// explicitly rather than auto-discovering from the filename.
type ToolOverride struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Script      string `json:"script"`
}

// AgentOverride lets the manifest pin an agent's metadata + tool allowlist.
type AgentOverride struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
}
