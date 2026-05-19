package launcher

// Chats are cloned instances of templates — one per work session. Each
// chat is fully self-contained: its own workpath copy, its own sandbox,
// its own MEMORY.md, its own per-launch session.json records.
//
// Layout under <root>/chats/<chat-id>/:
//
//   chat.json     {template, agent, createdAt, lastUsed, label}
//   workpath/     copied from <root>/templates/<Template>/workpath at creation
//   sandbox/      agent cwd; wpc-compiled output; gitignored
//   sessions/     one subdir per launch (was 'chats/' under Workspace)
//   MEMORY.md     persistent across re-opens of this chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ChatsDir is the conventional location for chats under root.
const ChatsDir = "chats"

// Chat is one cloned instance of a template, ready to run an agent against.
type Chat struct {
	// ID is the on-disk directory name (e.g. "20251017-1430-cve-fix").
	// We auto-derive it from the user's chat label + timestamp.
	ID    string
	Label string // human-friendly name the user typed

	Root        string // <workspacesRoot>/chats/<ID>
	WorkpathDir string // <Root>/workpath
	SandboxDir  string // <Root>/sandbox
	SessionsDir string // <Root>/sessions
	ChatJSON    string // <Root>/chat.json

	Template  string  // source template name (or "" if blank chat)
	AgentID   AgentID // locked at creation
	CreatedAt time.Time
	LastUsed  time.Time

	Description string            // copied from template at creation
	Settings    WorkspaceSettings // copied from template at creation, then mutable
}

type chatManifest struct {
	Label     string            `json:"label"`
	Template  string            `json:"template,omitempty"`
	Agent     string            `json:"agent"`
	CreatedAt time.Time         `json:"createdAt"`
	LastUsed  time.Time         `json:"lastUsed,omitempty"`
	Settings  WorkspaceSettings `json:"settings,omitempty"`
}

// ListChats returns every chat under <root>/chats/, sorted by LastUsed
// descending so the most recent chat is on top.
func ListChats(root string) ([]Chat, error) {
	dir := filepath.Join(root, ChatsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Chat
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		c, err := LoadChat(root, e.Name())
		if err != nil || c == nil {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out, nil
}

// LoadChat returns nil (no error) for a directory that isn't a well-formed
// chat (no chat.json).
func LoadChat(root, id string) (*Chat, error) {
	cRoot := filepath.Join(root, ChatsDir, id)
	manifestPath := filepath.Join(cRoot, "chat.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m chatManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	wpDir := filepath.Join(cRoot, "workpath")
	desc := ""
	if manifest, err := readManifest(filepath.Join(wpDir, "workpath.json")); err == nil {
		desc = manifest.Description
	}
	return &Chat{
		ID:          id,
		Label:       m.Label,
		Root:        cRoot,
		WorkpathDir: wpDir,
		SandboxDir:  filepath.Join(cRoot, "sandbox"),
		SessionsDir: filepath.Join(cRoot, "sessions"),
		ChatJSON:    manifestPath,
		Template:    m.Template,
		AgentID:     AgentID(m.Agent),
		CreatedAt:   m.CreatedAt,
		LastUsed:    m.LastUsed,
		Description: desc,
		Settings:    m.Settings,
	}, nil
}

var chatIDSanitize = regexp.MustCompile(`[^a-z0-9_-]+`)

// CreateChat clones a template into a fresh chat dir. The chat ID is a
// timestamp + slugified label so directory names are sortable and stable.
// Agent is locked at creation (the chat compiles for one agent target).
func CreateChat(root string, tpl Template, label string, agent AgentID) (Chat, error) {
	if strings.TrimSpace(label) == "" {
		return Chat{}, fmt.Errorf("chat label cannot be empty")
	}
	slug := slugifyLabel(label)
	if slug == "" {
		return Chat{}, fmt.Errorf("chat label %q has no usable characters", label)
	}
	now := time.Now().UTC()
	id := now.Format("20060102-150405") + "-" + slug
	cRoot := filepath.Join(root, ChatsDir, id)
	if _, err := os.Stat(cRoot); err == nil {
		// Should never happen given the timestamp prefix, but be safe.
		return Chat{}, fmt.Errorf("chat dir already exists: %s", cRoot)
	}
	if err := os.MkdirAll(cRoot, 0o755); err != nil {
		return Chat{}, err
	}

	// Clone the template's workpath verbatim.
	if err := copyTree(tpl.WorkpathDir, filepath.Join(cRoot, "workpath")); err != nil {
		_ = os.RemoveAll(cRoot)
		return Chat{}, fmt.Errorf("clone template workpath: %w", err)
	}

	// Inherit settings from the template at creation; mutable thereafter.
	manifest := chatManifest{
		Label:     label,
		Template:  tpl.Name,
		Agent:     string(agent),
		CreatedAt: now,
		LastUsed:  now,
		Settings:  tpl.Settings,
	}
	rawManifest, _ := json.MarshalIndent(manifest, "", "  ")
	rawManifest = append(rawManifest, '\n')
	if err := os.WriteFile(filepath.Join(cRoot, "chat.json"), rawManifest, 0o644); err != nil {
		_ = os.RemoveAll(cRoot)
		return Chat{}, err
	}

	chat := Chat{
		ID:          id,
		Label:       label,
		Root:        cRoot,
		WorkpathDir: filepath.Join(cRoot, "workpath"),
		SandboxDir:  filepath.Join(cRoot, "sandbox"),
		SessionsDir: filepath.Join(cRoot, "sessions"),
		ChatJSON:    filepath.Join(cRoot, "chat.json"),
		Template:    tpl.Name,
		AgentID:     agent,
		CreatedAt:   now,
		LastUsed:    now,
		Description: tpl.Description,
		Settings:    tpl.Settings,
	}
	return chat, nil
}

// DeleteChat removes the chat directory tree. Caller is expected to have
// confirmed with the user — there's no recovery.
func DeleteChat(root, id string) error {
	cRoot := filepath.Join(root, ChatsDir, id)
	if _, err := os.Stat(cRoot); err != nil {
		return err
	}
	return os.RemoveAll(cRoot)
}

// SaveChatSettings persists the mutable bits (lastUsed, settings) back to
// chat.json. Called after every launch and after a settings edit.
func SaveChatSettings(c Chat) error {
	manifest := chatManifest{
		Label:     c.Label,
		Template:  c.Template,
		Agent:     string(c.AgentID),
		CreatedAt: c.CreatedAt,
		LastUsed:  c.LastUsed,
		Settings:  c.Settings,
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := c.ChatJSON + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.ChatJSON)
}

// TouchChat updates LastUsed to now and persists. Called after a launch
// completes so the chat list resorts with most-recent-first.
func TouchChat(c *Chat) error {
	c.LastUsed = time.Now().UTC()
	return SaveChatSettings(*c)
}

// AsWorkspace converts a Chat into the legacy Workspace shape consumed
// by PrepareSandbox / Plan / decorate.go. Until those are migrated to
// take Chat directly, this bridge keeps the launch path one-function.
//
// Workspace.Name controls the compiled artifact name (.claude/skills/<Name>/,
// AGENTS.md heading, etc.). We use the *template* name so two chats cloned
// from "reversing" both produce a "reversing" skill — they don't collide
// because each chat has its own sandbox cwd. If the chat was created
// without a template, the chat ID is used as a unique fallback.
func (c Chat) AsWorkspace() Workspace {
	name := c.Template
	if name == "" {
		name = c.ID
	}
	return Workspace{
		Name:        name,
		Root:        c.Root,
		WorkpathDir: c.WorkpathDir,
		SandboxDir:  c.SandboxDir,
		ChatsDir:    c.SessionsDir,
		Description: c.Description,
		Settings:    c.Settings,
	}
}

// ValidateChatLabel returns nil if label slugifies to a non-empty
// identifier (the same transformation CreateChat applies), otherwise
// an error explaining what's wrong. TUI wizards call this on Enter so
// users see "no usable characters" inline instead of having the error
// bubble up and quit the launcher.
func ValidateChatLabel(label string) error {
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("chat name cannot be empty")
	}
	if slugifyLabel(label) == "" {
		return fmt.Errorf("chat name %q has no usable characters (letters, digits, '-', '_')", label)
	}
	return nil
}

func slugifyLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = chatIDSanitize.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if len(s) > 40 {
		s = s[:40]
		s = strings.TrimRight(s, "-_")
	}
	return s
}
