//go:build !windows

package main

import "os/exec"

// hideConsole is a no-op outside Windows. See procattr_windows.go.
func hideConsole(_ *exec.Cmd) {}
