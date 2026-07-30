// Package appdata owns PrAImate's persistent filesystem layout.
//
// Persistent application state lives under one root:
//
//	Linux:   $XDG_CONFIG_HOME/praimate (or ~/.config/praimate)
//	Windows: %APPDATA%\praimate
//
// User-selected project/workspace folders remain outside this root by design.
package appdata

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const DirName = "praimate"

// Root returns the canonical directory for all PrAImate-owned persistent data.
// PRAIMATE_HOME is primarily useful for portable installs and tests.
func Root() (string, error) {
	if root := os.Getenv("PRAIMATE_HOME"); root != "" {
		return filepath.Clean(root), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, DirName), nil
}

// LegacyRoots returns old PrAImate-owned roots that may still contain data.
// The canonical root is never included.
func LegacyRoots() []string {
	var roots []string
	home, _ := os.UserHomeDir()
	if runtime.GOOS != "windows" && home != "" {
		roots = append(roots, filepath.Join(home, ".praimate"))
	}
	if base, err := os.UserConfigDir(); err == nil {
		roots = append(roots, filepath.Join(base, "clade"))
	}
	if cache, err := os.UserCacheDir(); err == nil {
		roots = append(roots, filepath.Join(cache, DirName))
	}
	return roots
}
