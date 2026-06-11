package main

// `praimate --gui` dispatcher — launches PrAImate GUI, the Electron
// desktop app that lives under gui/ in this repo and installs from the
// per-OS packages on the GitHub release (NSIS .exe on Windows,
// .deb/.AppImage on Linux, .dmg on macOS).
//
// Resolution order:
//
//  1. The installed PrAImate GUI app in its platform's standard
//     install location (NSIS per-user dir on Windows, /opt on Linux
//     deb, /Applications on macOS)
//  2. praimate-gui on PATH (Linux deb installs a /usr/bin symlink;
//     also covers AppImage users who renamed it onto PATH)
//
// When neither exists we print the release-download pointer instead of
// failing cryptically.

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

// guiInstallCandidates returns the platform's standard install paths
// for the PrAImate GUI Electron app, most-preferred first.
func guiInstallCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		// electron-builder NSIS per-user install:
		//   %LOCALAPPDATA%\Programs\<productName>\<productName>.exe
		// Machine-wide installs land under Program Files.
		var out []string
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			out = append(out, filepath.Join(local, "Programs", "PrAImate GUI", "PrAImate GUI.exe"))
		}
		for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if pf != "" {
				out = append(out, filepath.Join(pf, "PrAImate GUI", "PrAImate GUI.exe"))
			}
		}
		return out
	case "darwin":
		return []string{"/Applications/PrAImate GUI.app/Contents/MacOS/PrAImate GUI"}
	default:
		// electron-builder .deb installs to /opt/<productName>/ with the
		// executable named after the package (praimate-gui).
		return []string{"/opt/PrAImate GUI/praimate-gui"}
	}
}

// resolveGUIBinary returns the path to the PrAImate GUI executable, or
// "" if not found. Split from launchGUI for testability.
func resolveGUIBinary() string {
	for _, candidate := range guiInstallCandidates() {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	name := "praimate-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// launchGUI starts PrAImate GUI detached and returns the process exit
// code for main() to pass through. The TUI process exits immediately
// after a successful spawn — the GUI owns its own lifetime; holding a
// parent terminal process open would just confuse `praimate --gui &`
// users.
func launchGUI() int {
	bin := resolveGUIBinary()
	if bin == "" {
		fmt.Fprintln(os.Stderr, "praimate: PrAImate GUI is not installed.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Download the installer for your OS from the latest release:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  https://github.com/sPROFFEs/PrAImate/releases/latest")
		fmt.Fprintln(os.Stderr)
		switch runtime.GOOS {
		case "windows":
			fmt.Fprintln(os.Stderr, "  → PrAImate-GUI.Setup.<version>.exe")
		case "darwin":
			fmt.Fprintln(os.Stderr, "  → PrAImate GUI-<version>-<arch>-mac.dmg")
		default:
			fmt.Fprintln(os.Stderr, "  → PrAImate GUI-<version>-<arch>.deb or .AppImage")
		}
		return 1
	}

	// Send the child's output to a log FILE, not a pipe: we exit right
	// after the grace period, and a pipe dies with us — the GUI would
	// then crash with EPIPE the next time it logs. A file handle stays
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
		fmt.Fprintln(os.Stderr, "praimate: PrAImate GUI exited immediately without opening a window.")
		if b, rerr := os.ReadFile(logPath); rerr == nil {
			if msg := strings.TrimSpace(string(b)); msg != "" {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, msg)
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
		fmt.Printf("launched PrAImate GUI (pid %d)\n", pid)
		return 0
	}
}
