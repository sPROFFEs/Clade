//go:build !windows

package main

import (
	"os/exec"
	"testing"
)

func TestHideRequirementsTerminalStartsNewSession(t *testing.T) {
	cmd := exec.Command("true")
	hideRequirementsTerminal(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatal("requirements process must start a new session without a controlling terminal")
	}
	if cmd.SysProcAttr.Setpgid {
		t.Fatal("Setsid already creates a process group; Setpgid must be disabled")
	}
}
