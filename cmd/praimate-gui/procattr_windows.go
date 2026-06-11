//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole stops helper subprocesses (e.g. `opencode models`) from
// flashing a console window — the GUI is a windowed app, so console
// children get a fresh visible console by default. Mirrors
// internal/core's procattr_windows.go.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
