// Package launcher holds the non-UI logic the waifu TUI calls into:
// user config, workspace discovery, agent CLI detection, and the
// compile-then-spawn sequence. The split exists so the logic is testable
// and reusable without dragging in Bubble Tea.
package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the persisted, per-user launcher state.
type Config struct {
	// WorkspacesRoot is the absolute path of the directory that contains
	// one subdirectory per workspace.
	WorkspacesRoot string `json:"workspacesRoot"`
	// LastAgent is surfaced as the default selection in the agent picker.
	LastAgent string `json:"lastAgent,omitempty"`
	// WpcBinary, when set, overrides PATH lookup. Optional.
	WpcBinary string `json:"wpcBinary,omitempty"`
}

// ConfigPaths returns the directory and file used for persistent config.
// On Linux this is $XDG_CONFIG_HOME/waifu/config.json (or ~/.config/...);
// on macOS, ~/Library/Application Support/waifu/...; on Windows,
// %AppData%/waifu/... — courtesy of os.UserConfigDir().
func ConfigPaths() (dir, file string, err error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("locate user config dir: %w", err)
	}
	dir = filepath.Join(base, "waifu")
	file = filepath.Join(dir, "config.json")
	return dir, file, nil
}

// LoadConfig returns (nil, nil) if the config file does not exist yet —
// callers treat that as the first-run signal.
func LoadConfig() (*Config, error) {
	_, file, err := ConfigPaths()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	return &c, nil
}

// SaveConfig writes the config atomically (write to .tmp + rename) so a
// crash mid-write doesn't corrupt the file.
func SaveConfig(c *Config) error {
	dir, file, err := ConfigPaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
