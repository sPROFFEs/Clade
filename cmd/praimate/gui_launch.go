package main

// Desktop dispatcher for the all-in-one entry point. The GUI stays a
// separate binary (praimate-gui) because the maintenance CLI remains
// console-friendly while Windows GUI subsystem flags suppress a console
// window for the desktop process. This bootstrap locates its sibling
// and launches it by default.
//
// Resolution order:
//
//  1. praimate-gui(.exe) in the same directory as this executable
//     (the release archives ship them side by side)
//  2. praimate-gui on PATH
//
// When neither exists, we print the build-from-source pointer instead
// of failing cryptically.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// resolveGUIBinary returns the path to praimate-gui, or "" if not
// found. Split from launchGUI for testability.
func resolveGUIBinary(exePath string) string {
	name := "praimate-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exePath != "" {
		sibling := filepath.Join(filepath.Dir(exePath), name)
		if fi, err := os.Stat(sibling); err == nil && !fi.IsDir() {
			return sibling
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// launchGUI starts praimate-gui detached and returns the process exit
// code for main() to pass through. The bootstrap process exits immediately
// after a successful spawn — the GUI owns its own lifetime; holding a
// parent terminal process open would just confuse background launches.
func launchGUI() int {
	exePath, _ := os.Executable()
	exePath, _ = filepath.EvalSymlinks(exePath)

	bin := resolveGUIBinary(exePath)
	if bin == "" {
		fmt.Fprintln(os.Stderr, "praimate: praimate-gui not found next to this binary or on PATH.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "The desktop GUI supports Linux and Windows.")
		fmt.Fprintln(os.Stderr, "To build it from source you need node+npm; on Linux also")
		fmt.Fprintln(os.Stderr, "libwebkit2gtk-4.1-dev and libgtk-3-dev:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  git clone https://github.com/sPROFFEs/praimate.git && cd praimate")
		fmt.Fprintln(os.Stderr, "  cd cmd/praimate-gui && ./build.sh")
		return 1
	}

	// Remove the diagnostic file written by pre-log-free releases.
	_ = os.Remove(filepath.Join(os.TempDir(), "praimate-gui-launch.log"))

	cmd := exec.Command(bin)
	// The desktop launcher deliberately discards child output. Persistent
	// diagnostic logs can contain paths, prompts, or provider errors and are
	// not part of PrAImate's data model.
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "praimate: failed to start %s: %v\n", bin, err)
		return 1
	}
	pid := cmd.Process.Pid

	// Wait briefly. If the GUI is still alive after the grace period it
	// almost certainly opened (or is opening) its window, so we detach.
	// If it exited, surface why.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		fmt.Fprintln(os.Stderr, "praimate: praimate-gui exited immediately without opening a window.")
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
		}
		return 1
	case <-time.After(1500 * time.Millisecond):
		_ = cmd.Process.Release()
		fmt.Printf("launched praimate-gui (pid %d)\n", pid)
		return 0
	}
}
