// Package main is the deprecation shim for the legacy `clade` binary.
// PrAImate 1.0 replaces Clade. This shim prints a notice and runs
// the `praimate` binary if it is on PATH, so muscle-memory invocations
// keep working for one release cycle.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	fmt.Fprintln(os.Stderr, "clade has been renamed to praimate (PrAImate 1.0).")
	fmt.Fprintln(os.Stderr, "This shim will be removed in 1.1. Please run `praimate` instead.")
	fmt.Fprintln(os.Stderr)

	path, err := exec.LookPath("praimate")
	if err != nil {
		fmt.Fprintln(os.Stderr, "praimate not found on PATH. Install it from:")
		fmt.Fprintln(os.Stderr, "  https://github.com/sPROFFEs/PrAImate/releases/latest")
		os.Exit(127)
	}

	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "failed to run praimate:", err)
		os.Exit(1)
	}
}
