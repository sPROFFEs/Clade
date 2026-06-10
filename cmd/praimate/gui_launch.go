package main

// `praimate --gui` dispatcher — the AIO entry point. The GUI stays a
// separate binary (praimate-gui) because a true single binary can't
// work everywhere: on Linux, linking webkit into the TUI would make
// it fail to start on headless boxes; on Windows, console-vs-GUI
// subsystem flags force a choice that breaks one of the two modes.
// Instead the TUI binary locates its sibling and launches it, so
// users get one command for both surfaces.
//
// Resolution order:
//
//  1. praimate-gui(.exe) in the same directory as this executable
//     (the release archives ship them side by side)
//  2. praimate-gui on PATH
//
// When neither exists (linux-arm64 / macOS archives, or a
// build-from-source TUI), we print the build-from-source pointer
// instead of failing cryptically.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// code for main() to pass through. The TUI process exits immediately
// after a successful spawn — the GUI owns its own lifetime; holding a
// parent terminal process open would just confuse `praimate --gui &`
// users.
func launchGUI() int {
	exePath, _ := os.Executable()
	exePath, _ = filepath.EvalSymlinks(exePath)

	bin := resolveGUIBinary(exePath)
	if bin == "" {
		fmt.Fprintln(os.Stderr, "praimate: praimate-gui not found next to this binary or on PATH.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "The desktop GUI ships prebuilt for linux-amd64 and windows-amd64.")
		fmt.Fprintln(os.Stderr, "On other platforms build it from source (needs node+npm; on Linux")
		fmt.Fprintln(os.Stderr, "also libwebkit2gtk-4.1-dev libgtk-3-dev):")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  git clone https://github.com/sPROFFEs/PrAImate && cd PrAImate")
		fmt.Fprintln(os.Stderr, "  cd cmd/praimate-gui && ./build.sh")
		return 1
	}

	cmd := exec.Command(bin)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "praimate: failed to start %s: %v\n", bin, err)
		return 1
	}
	pid := cmd.Process.Pid
	// Detach: don't wait. Release the process handle so the GUI keeps
	// running after this dispatcher exits.
	_ = cmd.Process.Release()
	fmt.Printf("launched praimate-gui (pid %d)\n", pid)
	return 0
}
