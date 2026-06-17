//go:build !windows

package core

import (
	"os/exec"
	"syscall"
)

// hideConsole is a no-op on non-Windows platforms — children inherit
// the parent's stdio without spawning a window. See procattr_windows.go.
// It DOES set the process to its own group so context cancellation
// (the Stop button) can syscall.Kill(-pid) and bring down the whole
// tree — without Setpgid the wrapped node/bun CLI's children survive
// the SIGKILL Go sends to the immediate child.
func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	// Cancel handler: kill the whole group, not just the immediate
	// child. exec.CommandContext defaults to .Process.Kill() which on
	// Linux is os.Process.Kill() — that's SIGKILL to PID, not -PID.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
