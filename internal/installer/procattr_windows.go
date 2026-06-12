//go:build windows

package installer

import (
	"os/exec"
	"syscall"
)

// createNoWindow stops Windows from allocating a console for the child.
// Without it, every per-turn CLI spawn from a GUI process flashes a
// black console window. HideWindow alone is not enough for console-
// subsystem children; CREATE_NO_WINDOW suppresses the console entirely.
const createNoWindow = 0x08000000

// hideConsole configures cmd so the child process never opens a visible
// console window. Must be called before cmd.Start/Run.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
