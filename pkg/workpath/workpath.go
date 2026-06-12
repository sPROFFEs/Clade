// Package workpath defines the in-memory representation of a workpath
// (also called a skill-path): a self-contained bundle describing a mission,
// staged playbook, hard rules, executable tools, and named subagents.
//
// The source-on-disk format is a directory:
//
//	<name>/
//	  workpath.json   (optional metadata: description, version, tool/agent overrides, imports)
//	  mission.md      (required: what this workpath is for)
//	  playbook.md     (optional: staged process)
//	  rules.md        (optional: hard constraints)
//	  tools/*.sh|*.ps1 (optional: executables, auto-registered)
//	  agents/*.md     (optional: subagent prompts)
//	  knowledge/      (optional: reference material; recursive)
//
// Imports: a workpath.json can declare `imports: ["_common/graphify"]` to pull
// shared capability bundles (knowledge + tools + agents + playbook-fragment.md
// + rules-fragment.md) from sibling directories. Imports are resolved relative
// to the parent of the workpath's own dir (the "templates root") and merged
// in-memory at load time. Template entries override imported ones on name /
// path collisions. See LoadImport.
//
// The Workpath struct is target-agnostic. Per-target compilation lives in
// pkg/targets.
package workpath

import (
	"path/filepath"
)

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

	// Hooks are chat-lifecycle triggers loaded from <workpath>/hooks.json
	// (and from imported bundles). Each target compiles them into its
	// native hook format; targets without a documented hook system emit
	// a note so authors aren't surprised by silent drops.
	Hooks []Hook

	// Knowledge is the reference material under <workpath>/knowledge/ —
	// docs, papers, tool descriptions the agent can read on demand
	// when reasoning. Auto-discovered recursively from any files in
	// that subdir. Text-ish files (md, txt, rst, org, json, yaml,
	// markdown variants) get a short summary preview extracted at
	// load time so the agent can decide which to open; everything
	// else is listed by name only.
	Knowledge []KnowledgeFile

	// SourceDir is the absolute path the workpath was loaded from. Targets
	// use this to resolve relative script/agent paths.
	SourceDir string
}

// KnowledgeFile is one item from <workpath>/knowledge/. Targets copy
// it into the sandbox at the same relative path so the agent's
// file-reading tools find it; the manifest block in the compiled
// instructions tells the agent what's there.
type KnowledgeFile struct {
	// RelPath is the file path relative to the workpath root, e.g.
	// "knowledge/papers/secure-boot.md". Always uses forward slashes.
	RelPath string
	// Title is the H1 heading (for markdown) or the filename base
	// otherwise. Used in the manifest's display.
	Title string
	// Summary is the first paragraph / first non-blank line of a
	// text file (capped at ~280 chars). Empty for non-text files.
	Summary string
	// Bytes is the on-disk size in bytes.
	Bytes int64
	// IsText flags whether this file's contents are AI-legible text.
	// False for PDFs, images, archives, etc. — listed by name only.
	IsText bool

	// ImportedFrom, when non-empty, is the absolute path to the IMPORT'S
	// SourceDir that contributed this knowledge file. Targets resolve
	// RelPath relative to ImportedFrom instead of the parent Workpath.SourceDir.
	ImportedFrom string `json:"-"`
}

// ResolveToolScript returns the absolute on-disk path of a tool script
// (one of tool.Script or any entry in tool.Scripts). For imported tools
// the script lives under the import's source dir; for native tools
// under the workpath's own source dir.
func (w *Workpath) ResolveToolScript(t Tool, scriptRel string) string {
	base := w.SourceDir
	if t.ImportedFrom != "" {
		base = t.ImportedFrom
	}
	return filepath.Join(base, filepath.FromSlash(scriptRel))
}

// ResolveAgentPrompt returns the absolute on-disk path of an agent
// prompt. Same logic as ResolveToolScript.
func (w *Workpath) ResolveAgentPrompt(a Agent) string {
	base := w.SourceDir
	if a.ImportedFrom != "" {
		base = a.ImportedFrom
	}
	return filepath.Join(base, filepath.FromSlash(a.Prompt))
}

// ResolveKnowledgePath returns the absolute on-disk path of a knowledge
// file. Same logic as ResolveToolScript.
func (w *Workpath) ResolveKnowledgePath(k KnowledgeFile) string {
	base := w.SourceDir
	if k.ImportedFrom != "" {
		base = k.ImportedFrom
	}
	return filepath.Join(base, filepath.FromSlash(k.RelPath))
}

// Tool is a shell script registered as an agent-callable command.
//
// A single logical tool may have multiple script files when the author
// supplies platform-paired variants (e.g. inventory.sh + inventory.ps1
// living side-by-side). Loaders merge these into one Tool: Script holds
// the primary (.sh preferred) for back-compat, Scripts holds every
// script file targets should copy into the sandbox.
type Tool struct {
	Name        string   // identifier (e.g. "file_summary")
	Description string   // single line, surfaced to the agent
	Script      string   // primary script path (e.g. "tools/file_summary.sh")
	Scripts     []string // all script files; len > 1 = platform-paired variants
	Shell       string   // "bash" for .sh, "pwsh" for .ps1; inferred from Script

	// ImportedFrom, when non-empty, is the absolute path to the IMPORT'S
	// SourceDir that contributed this tool. Targets resolve Script/Scripts
	// relative to ImportedFrom instead of the parent Workpath.SourceDir.
	// Set only by mergeImports; empty for tools native to the consumer.
	ImportedFrom string `json:"-"`
}

// AllScripts returns every script path the tool ships. Equivalent to
// Scripts when set; falls back to [Script] for back-compat / manifest-
// declared tools.
func (t Tool) AllScripts() []string {
	if len(t.Scripts) > 0 {
		return t.Scripts
	}
	if t.Script != "" {
		return []string{t.Script}
	}
	return nil
}

// HookEvent is a portable PrAImate-side event name. Each target maps these
// to its own native hook vocabulary (or returns "unsupported" for
// targets without a stable hook system yet — codex, opencode, gemini,
// mika). The names mirror Claude Code's events translated to
// snake_case so the source schema stays target-agnostic.
type HookEvent string

const (
	HookPreTool      HookEvent = "pre_tool"      // before a tool call (Claude: PreToolUse)
	HookPostTool     HookEvent = "post_tool"     // after a tool call (Claude: PostToolUse)
	HookUserInput    HookEvent = "user_input"    // after the user submits a message (Claude: UserPromptSubmit)
	HookSessionStart HookEvent = "session_start" // at session start (Claude: SessionStart)
	HookSessionStop  HookEvent = "session_stop"  // at session stop (Claude: Stop)
	HookSubagentStop HookEvent = "subagent_stop" // when a subagent finishes (Claude: SubagentStop)
	HookNotification HookEvent = "notification"  // on a notification event (Claude: Notification)
)

// AllHookEvents is the validation allowlist. Anything not in this set
// fails workpath.Validate.
var AllHookEvents = map[HookEvent]bool{
	HookPreTool: true, HookPostTool: true, HookUserInput: true,
	HookSessionStart: true, HookSessionStop: true,
	HookSubagentStop: true, HookNotification: true,
}

// Hook is a single trigger → command pair the agent runs as a chat
// lifecycle event fires. Source is a hooks.json file at the workpath
// root or inside an imported bundle.
type Hook struct {
	// Event is the portable PrAImate event name. See HookEvent constants.
	Event HookEvent `json:"event"`
	// Matcher is the optional pattern that gates the hook. For tool
	// events (pre_tool / post_tool) it matches the tool name (regex
	// or literal — target-defined; Claude Code accepts regex). Empty
	// matcher = fire on every occurrence of the event. Ignored for
	// non-tool events.
	Matcher string `json:"matcher,omitempty"`
	// Command is the shell command run when the event fires. Resolved
	// relative to the agent's cwd (the sandbox); script references
	// like "scripts/audit.sh" find the wpc-staged tool dir.
	Command string `json:"command"`
	// Description is a one-line human note; surfaced in the compiled
	// instruction body so the agent knows what the hook does.
	Description string `json:"description,omitempty"`

	// ImportedFrom mirrors the convention on Tool / Agent / KnowledgeFile:
	// non-empty when the hook came from a workpath import. Collision
	// rule: consumer wins (same event + matcher key).
	ImportedFrom string `json:"-"`
}

// HooksManifest is the on-disk shape of hooks.json — just an array
// wrapped in {"hooks": [...]} so the file format is extensible.
type HooksManifest struct {
	Hooks []Hook `json:"hooks"`
}

// Agent is a named subagent prompt with an optional tool allowlist.
type Agent struct {
	Name        string   // identifier (e.g. "triage")
	Description string   // single line, surfaced when the parent agent picks a subagent
	Prompt      string   // path relative to SourceDir (e.g. "agents/triage.md")
	Tools       []string // tool names this subagent may use; empty = all

	// ImportedFrom, when non-empty, is the absolute path to the IMPORT'S
	// SourceDir that contributed this agent. Targets resolve Prompt
	// relative to ImportedFrom instead of the parent Workpath.SourceDir.
	ImportedFrom string `json:"-"`
}

// Manifest is the on-disk shape of workpath.json. All fields are optional;
// see workpath_test.go for examples.
type Manifest struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version,omitempty"`
	License     string          `json:"license,omitempty"`
	Tools       []ToolOverride  `json:"tools,omitempty"`
	Agents      []AgentOverride `json:"agents,omitempty"`

	// Imports are paths to sibling capability bundles to merge into this
	// workpath at load time. Each path is relative to the parent of the
	// workpath's own dir (the "templates root"). Example:
	//
	//   "imports": ["_common/graphify"]
	//
	// pulls knowledge/, tools/, agents/, playbook-fragment.md,
	// rules-fragment.md from <templates-root>/_common/graphify/.
	// Template entries override imports on name / path collisions.
	Imports []string `json:"imports,omitempty"`
}

// Import is the parsed shape of an imported capability bundle. Unlike a
// Workpath, an Import has no mission.md / playbook.md / rules.md — it
// contributes only knowledge, tools, agents, and optional fragment
// files that extend the consumer's playbook + rules.
type Import struct {
	// Name is the import identifier (derived from the directory basename).
	Name string
	// SourceDir is the absolute path to the import's directory.
	SourceDir string
	// PlaybookFragment is the body of playbook-fragment.md, appended to
	// the consumer's Playbook under a "## Imported capabilities" heading.
	PlaybookFragment string
	// RulesFragment is the body of rules-fragment.md, appended to the
	// consumer's Rules under "## Imported rules".
	RulesFragment string
	// Tools, Agents, Knowledge, Hooks are discovered the same way as a
	// Workpath. Hooks come from <bundle>/hooks.json if present.
	Tools     []Tool
	Agents    []Agent
	Knowledge []KnowledgeFile
	Hooks     []Hook
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
