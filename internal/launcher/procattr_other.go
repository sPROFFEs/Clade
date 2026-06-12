//go:build !windows

package launcher

import "os/exec"

// hideConsole is a no-op on non-Windows platforms — children inherit
// the parent's stdio without spawning a window. See procattr_windows.go.
func hideConsole(_ *exec.Cmd) {}
