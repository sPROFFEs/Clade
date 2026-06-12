//go:build windows

package backup

import (
	"os/exec"
	"syscall"
)

// createNoWindow stops Windows from allocating a console for the child.
// A single Sync runs a dozen git invocations (status, fetch, rev-list,
// log, add, commit, push…) — without this, every one of them flashes a
// black console window when the caller is a GUI process. HideWindow
// alone is not enough for console-subsystem children; CREATE_NO_WINDOW
// suppresses the console entirely.
const createNoWindow = 0x08000000

// hideConsole configures cmd so the child process never opens a visible
// console window. Must be called before cmd.Start/Run.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
