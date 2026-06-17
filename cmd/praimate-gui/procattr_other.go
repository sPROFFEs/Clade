//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole puts the child in its own process group so context
// cancellation (the Stop button on the Chats page, helper teardown,
// etc.) brings down the entire wrapped-CLI tree instead of leaving
// node/bun grandchildren running. No console-hiding work to do on
// Linux/macOS — that part is Windows-only. Mirrors core's other-build.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
