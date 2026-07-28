package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultDBPath returns the canonical PrAImate database location for
// the current OS. Callers should pass this to Open() unless the user
// has overridden it.
//
// Layout:
//
//	Linux:   ~/.praimate/db.sqlite
//	Windows: %APPDATA%/PrAImate/db.sqlite
func DefaultDBPath() (string, error) {
	if env := os.Getenv("PRAIMATE_DB"); env != "" {
		return env, nil
	}
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA not set")
		}
		return filepath.Join(appdata, "PrAImate", "db.sqlite"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".praimate", "db.sqlite"), nil
}
