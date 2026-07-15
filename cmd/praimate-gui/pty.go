package main

// Terminal sessions — host a real third-party CLI (claude / codex /
// opencode / …) live inside the GUI via a PTY. This is what makes the
// chat "fully functional for coding": you get the actual CLI's
// interactive TUI — streaming tokens, tool calls, file edits,
// approvals — running in a project folder, not a reimplemented loop.
//
// Lifecycle (all driven from the frontend over Wails bindings):
//   StartTerminal  → spawn CLI in a PTY, stream output via events
//   WriteTerminal  → forward keystrokes
//   ResizeTerminal → keep the PTY's window size in sync with xterm.js
//   CloseTerminal  → kill the child and clean up
//
// Output is emitted as Wails events named "term:data:<id>" (base64 so
// arbitrary bytes survive JSON) and a one-shot "term:exit:<id>".

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aymanbagabas/go-pty"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type termSession struct {
	id           string
	chatID       string // empty for terminals not bound to a chat row
	cwd          string
	name         string
	pty          pty.Pty
	cmd          *pty.Cmd
	once         sync.Once
	history      []byte
	historyStart int64
	outputEnd    int64
}

const terminalHistoryLimit = 4 << 20

// TerminalData is one offset-addressed PTY output chunk. Offsets let a newly
// mounted xterm replay the snapshot and de-duplicate output that arrived while
// the snapshot request was in flight.
type TerminalData struct {
	Data        string `json:"data"`
	StartOffset int64  `json:"startOffset"`
	EndOffset   int64  `json:"endOffset"`
}

// TerminalSnapshot is the retained tail of a live terminal's byte stream.
type TerminalSnapshot struct {
	Data        string `json:"data"`
	StartOffset int64  `json:"startOffset"`
	EndOffset   int64  `json:"endOffset"`
}

// TermInfo is a snapshot of one live PTY session for the Sessions panel.
type TermInfo struct {
	ID     string `json:"id"`
	ChatID string `json:"chatId,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	Name   string `json:"name,omitempty"`
}

type termManager struct {
	mu         sync.Mutex
	sessions   map[string]*termSession
	seq        int
	historyDir string
}

func newTermManager() *termManager {
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		cacheDir = os.TempDir()
	}
	return &termManager{
		sessions:   map[string]*termSession{},
		historyDir: filepath.Join(cacheDir, "praimate", "code-history"),
	}
}

func (tm *termManager) removeHistory(chatID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	path, err := tm.historyPath(chatID)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// list returns a snapshot of every live PTY session so the Sessions
// panel can match chats to their running terminals and offer "resume"
// instead of "open new".
func (tm *termManager) list() []TermInfo {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]TermInfo, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		out = append(out, TermInfo{ID: s.id, ChatID: s.chatID, Cwd: s.cwd, Name: s.name})
	}
	return out
}

// bindChat associates a live PTY with a chat row. Called after a chat is
// created for a terminal session so the Sessions panel can resume the
// PTY by chat ID.
func (tm *termManager) bindChat(id, chatID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	s := tm.sessions[id]
	if s == nil {
		return fmt.Errorf("no terminal %q", id)
	}
	if s.chatID == chatID {
		return nil
	}
	if s.chatID != "" {
		return fmt.Errorf("terminal %q is already bound to chat %q", id, s.chatID)
	}
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("chat id is required")
	}
	// Output may arrive before RecordCodeSession finishes. Persist that
	// retained prefix first, then recordOutput appends every later chunk.
	if err := tm.appendHistoryLocked(chatID, s.history); err != nil {
		return err
	}
	s.chatID = chatID
	return nil
}

func (tm *termManager) recordOutput(id string, data []byte) (TerminalData, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	s := tm.sessions[id]
	if s == nil {
		return TerminalData{}, false
	}
	start := s.outputEnd
	s.outputEnd += int64(len(data))
	s.history = append(s.history, data...)
	if len(s.history) > terminalHistoryLimit {
		drop := len(s.history) - terminalHistoryLimit
		s.history = append([]byte(nil), s.history[drop:]...)
		s.historyStart += int64(drop)
	}
	if s.chatID != "" {
		// Keep the file write under the same lock as outputEnd. This makes a
		// codeSnapshot atomic with respect to both the persistent transcript
		// and the offset used to de-duplicate queued frontend events.
		_ = tm.appendHistoryLocked(s.chatID, data)
	}
	return TerminalData{
		Data:        base64.StdEncoding.EncodeToString(data),
		StartOffset: start,
		EndOffset:   s.outputEnd,
	}, true
}

// codeSnapshot returns the persisted transcript for a Code chat together
// with the current live process offset. The frontend subscribes before this
// call, writes Data once, then drops queued chunks ending at EndOffset.
func (tm *termManager) codeSnapshot(chatID, termID string) (TerminalSnapshot, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var end int64
	if termID != "" {
		s := tm.sessions[termID]
		if s == nil {
			return TerminalSnapshot{}, fmt.Errorf("no terminal %q", termID)
		}
		if s.chatID != chatID {
			return TerminalSnapshot{}, fmt.Errorf("terminal %q is not bound to chat %q", termID, chatID)
		}
		end = s.outputEnd
	}
	raw, err := tm.readHistoryLocked(chatID)
	if err != nil {
		return TerminalSnapshot{}, err
	}
	return TerminalSnapshot{
		Data:      base64.StdEncoding.EncodeToString(raw),
		EndOffset: end,
	}, nil
}

func (tm *termManager) historyPath(chatID string) (string, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return "", fmt.Errorf("chat id is required")
	}
	name := base64.RawURLEncoding.EncodeToString([]byte(chatID)) + ".log"
	return filepath.Join(tm.historyDir, name), nil
}

func (tm *termManager) appendHistoryLocked(chatID string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	path, err := tm.historyPath(chatID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tm.historyDir, 0o755); err != nil {
		return fmt.Errorf("create terminal history directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open terminal history: %w", err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write terminal history: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close terminal history: %w", closeErr)
	}

	// Avoid rewriting on every small PTY chunk once the limit is reached.
	// Let the file grow to 5 MiB, then compact it back to the latest 4 MiB.
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > terminalHistoryLimit+(1<<20) {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("compact terminal history: %w", readErr)
		}
		if len(raw) > terminalHistoryLimit {
			raw = raw[len(raw)-terminalHistoryLimit:]
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return fmt.Errorf("compact terminal history: %w", err)
		}
	}
	return nil
}

func (tm *termManager) readHistoryLocked(chatID string) ([]byte, error) {
	path, err := tm.historyPath(chatID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read terminal history: %w", err)
	}
	if len(raw) > terminalHistoryLimit {
		raw = raw[len(raw)-terminalHistoryLimit:]
	}
	return raw, nil
}

func (tm *termManager) snapshot(id string) (TerminalSnapshot, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	s := tm.sessions[id]
	if s == nil {
		return TerminalSnapshot{}, fmt.Errorf("no terminal %q", id)
	}
	return TerminalSnapshot{
		Data:        base64.StdEncoding.EncodeToString(s.history),
		StartOffset: s.historyStart,
		EndOffset:   s.outputEnd,
	}, nil
}

// start spawns name with args in cwd inside a new PTY. env adds to the
// inherited environment. PTY output streams to the frontend over Wails
// events on emitCtx until the child exits.
func (tm *termManager) start(emitCtx context.Context, name string, args []string, cwd string, env []string) (string, error) {
	// Resolve to an absolute path BEFORE handing it to go-pty / Cmd.
	// Go's exec on Windows joins Cmd.Dir + bare name first when
	// resolving — so a bare `opencode` with Cmd.Dir set to the project
	// folder produces errors like
	//   exec: "C:\…\<project>\opencode": not found in %PATH%
	// even though opencode IS on PATH, just not in <project>. Pre-
	// resolving collapses the ambiguity on every platform.
	resolved, lpErr := exec.LookPath(name)
	if lpErr != nil {
		return "", fmt.Errorf("%s not on PATH — install it (CLIs tab) or click 'Re-scan PATH' if you just installed it in another terminal", name)
	}
	p, err := pty.New()
	if err != nil {
		return "", fmt.Errorf("open pty: %w", err)
	}
	c := p.Command(resolved, args...)
	c.Dir = cwd
	// Cmd.Env=nil inherits the parent, but assigning only our MCP/local-LLM
	// overlay replaces the environment completely. That used to discard
	// LANG/LC_* (and HOME/PATH), making interactive CLIs interpret UTF-8 input
	// such as accents as unrelated control/glyph bytes. Always merge overlays
	// into the inherited environment and guarantee a UTF-8 terminal locale.
	c.Env = terminalEnvironment(os.Environ(), env)
	if err := c.Start(); err != nil {
		_ = p.Close()
		return "", fmt.Errorf("start %s: %w", name, err)
	}

	tm.mu.Lock()
	tm.seq++
	id := fmt.Sprintf("term-%d", tm.seq)
	tm.sessions[id] = &termSession{id: id, pty: p, cmd: c, cwd: cwd, name: name}
	tm.mu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := p.Read(buf)
			if n > 0 {
				if chunk, ok := tm.recordOutput(id, buf[:n]); ok {
					wruntime.EventsEmit(emitCtx, "term:data:"+id, chunk)
				}
			}
			if rerr != nil {
				break
			}
		}
		_ = c.Wait()
		wruntime.EventsEmit(emitCtx, "term:exit:"+id)
		tm.close(id)
	}()

	return id, nil
}

// terminalEnvironment merges a small launch overlay into the host process
// environment. Overrides win without duplicating keys. Interactive terminal
// programs need a UTF-8 locale even when PrAImate was launched from a desktop
// entry with a sparse or legacy C locale.
func terminalEnvironment(base, overrides []string) []string {
	out := make([]string, 0, len(base)+len(overrides)+3)
	index := make(map[string]int, len(base)+len(overrides))
	put := func(kv string) {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return
		}
		if i, exists := index[key]; exists {
			out[i] = kv
			return
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range base {
		put(kv)
	}
	for _, kv := range overrides {
		put(kv)
	}
	value := func(key string) string {
		i, ok := index[key]
		if !ok {
			return ""
		}
		_, v, _ := strings.Cut(out[i], "=")
		return v
	}
	if value("TERM") == "" {
		put("TERM=xterm-256color")
	}
	if value("COLORTERM") == "" {
		put("COLORTERM=truecolor")
	}

	locale := value("LC_ALL")
	if locale == "" {
		locale = value("LC_CTYPE")
	}
	if locale == "" {
		locale = value("LANG")
	}
	normalized := strings.ReplaceAll(strings.ToUpper(locale), "-", "")
	if !strings.Contains(normalized, "UTF8") {
		if value("LC_ALL") != "" {
			put("LC_ALL=C.UTF-8")
		} else {
			put("LC_CTYPE=C.UTF-8")
		}
		if value("LANG") == "" {
			put("LANG=C.UTF-8")
		}
	}
	return out
}

func (tm *termManager) write(id string, data []byte) error {
	tm.mu.Lock()
	s := tm.sessions[id]
	tm.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no terminal %q", id)
	}
	_, err := s.pty.Write(data)
	return err
}

func (tm *termManager) resize(id string, cols, rows int) error {
	tm.mu.Lock()
	s := tm.sessions[id]
	tm.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no terminal %q", id)
	}
	return s.pty.Resize(cols, rows)
}

func (tm *termManager) close(id string) {
	tm.mu.Lock()
	s := tm.sessions[id]
	delete(tm.sessions, id)
	tm.mu.Unlock()
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		if s.pty != nil {
			_ = s.pty.Close()
		}
	})
}

func (tm *termManager) closeAll() {
	tm.mu.Lock()
	ids := make([]string, 0, len(tm.sessions))
	for id := range tm.sessions {
		ids = append(ids, id)
	}
	tm.mu.Unlock()
	for _, id := range ids {
		tm.close(id)
	}
}
