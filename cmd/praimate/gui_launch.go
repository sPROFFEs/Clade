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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

	// Send the child's output to a log FILE, not a pipe: we exit right
	// after the grace period, and a pipe dies with us — the GUI would
	// then hit write errors the next time it logs. A file handle stays
	// valid after we're gone, and doubles as the diagnostic source when
	// the GUI exits immediately.
	logPath := filepath.Join(os.TempDir(), "praimate-gui-launch.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		logFile = nil // degrade: discard output rather than fail launch
	}
	cmd := exec.Command(bin)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "praimate: failed to start %s: %v\n", bin, err)
		return 1
	}
	if logFile != nil {
		logFile.Close() // child holds its own handle now
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
		if b, rerr := os.ReadFile(logPath); rerr == nil {
			if msg := strings.TrimSpace(string(b)); msg != "" {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, msg)
				if strings.Contains(msg, "build tags") {
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "This looks like an older praimate-gui build. Reinstall PrAImate 1.1.1+")
					fmt.Fprintln(os.Stderr, "or rebuild the GUI: cd cmd/praimate-gui && ./build.sh")
				}
			}
		}
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
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
