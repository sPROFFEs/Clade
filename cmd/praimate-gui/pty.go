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
	"sync"

	"github.com/aymanbagabas/go-pty"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type termSession struct {
	id   string
	pty  pty.Pty
	cmd  *pty.Cmd
	once sync.Once
}

type termManager struct {
	mu       sync.Mutex
	sessions map[string]*termSession
	seq      int
}

func newTermManager() *termManager {
	return &termManager{sessions: map[string]*termSession{}}
}

// start spawns name with args in cwd inside a new PTY. env adds to the
// inherited environment. PTY output streams to the frontend over Wails
// events on emitCtx until the child exits.
func (tm *termManager) start(emitCtx context.Context, name string, args []string, cwd string, env []string) (string, error) {
	p, err := pty.New()
	if err != nil {
		return "", fmt.Errorf("open pty: %w", err)
	}
	c := p.Command(name, args...)
	c.Dir = cwd
	if len(env) > 0 {
		c.Env = append(c.Env, env...)
	}
	if err := c.Start(); err != nil {
		_ = p.Close()
		return "", fmt.Errorf("start %s: %w", name, err)
	}

	tm.mu.Lock()
	tm.seq++
	id := fmt.Sprintf("term-%d", tm.seq)
	tm.sessions[id] = &termSession{id: id, pty: p, cmd: c}
	tm.mu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := p.Read(buf)
			if n > 0 {
				wruntime.EventsEmit(emitCtx, "term:data:"+id, base64.StdEncoding.EncodeToString(buf[:n]))
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
		_ = s.pty.Close()
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
