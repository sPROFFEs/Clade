package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigRoundTrip exercises save then load. We can't use the real
// user-config dir (would pollute the developer's machine) so we
// override the relevant env var per platform. os.UserConfigDir honors
// XDG_CONFIG_HOME on Linux, HOME on macOS (for Library/...), and
// APPDATA on Windows.
func TestConfigRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	switch {
	case envHasKey("XDG_CONFIG_HOME") || isLinux():
		t.Setenv("XDG_CONFIG_HOME", tmp)
	case isWindows():
		t.Setenv("APPDATA", tmp)
	default:
		// macOS or unknown: os.UserConfigDir falls back to $HOME/Library/...
		t.Setenv("HOME", tmp)
	}

	want := &Config{WorkspacesRoot: "/tmp/ws-root", LastAgent: "codex"}
	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got == nil {
		t.Fatal("LoadConfig returned nil after save")
	}
	if got.WorkspacesRoot != want.WorkspacesRoot || got.LastAgent != want.LastAgent {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}

	// Verify the path lives under the override root.
	_, file, err := ConfigPaths()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(tmp, file)
	if err != nil || rel == "" || rel == "." || rel[:2] == ".." {
		t.Errorf("config file %q not under override root %q", file, tmp)
	}
}

func TestLoadConfig_MissingReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	switch {
	case isLinux():
		t.Setenv("XDG_CONFIG_HOME", tmp)
	case isWindows():
		t.Setenv("APPDATA", tmp)
	default:
		t.Setenv("HOME", tmp)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on missing file, got %+v", got)
	}
}

func envHasKey(k string) bool {
	_, ok := os.LookupEnv(k)
	return ok
}

// TestHasLocalDefault covers the gate the new-chat wizard uses to decide
// whether to offer the saved-endpoint shortcut, and that the three
// DefaultLocal* fields round-trip through save/load.
func TestHasLocalDefault(t *testing.T) {
	if (&Config{}).HasLocalDefault() {
		t.Error("empty config should not report a local default")
	}
	if (*Config)(nil).HasLocalDefault() {
		t.Error("nil config should not report a local default")
	}
	if !(&Config{DefaultLocalEndpoint: "http://x:11434"}).HasLocalDefault() {
		t.Error("config with endpoint should report a local default")
	}

	tmp := t.TempDir()
	switch {
	case envHasKey("XDG_CONFIG_HOME") || isLinux():
		t.Setenv("XDG_CONFIG_HOME", tmp)
	case isWindows():
		t.Setenv("APPDATA", tmp)
	default:
		t.Setenv("HOME", tmp)
	}
	want := &Config{
		WorkspacesRoot:            "/tmp/ws",
		DefaultLocalEndpoint:      "http://192.168.1.50:11434",
		DefaultLocalAPIKey:        "secret",
		DefaultLocalWireAPI:       "responses",
		DefaultLocalContextTokens: 4096,
		DefaultLocalOutputTokens:  1024,
	}
	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig()
	if err != nil || got == nil {
		t.Fatalf("LoadConfig: %v (got=%v)", err, got)
	}
	if got.DefaultLocalEndpoint != want.DefaultLocalEndpoint ||
		got.DefaultLocalAPIKey != want.DefaultLocalAPIKey ||
		got.DefaultLocalWireAPI != want.DefaultLocalWireAPI ||
		got.DefaultLocalContextTokens != want.DefaultLocalContextTokens ||
		got.DefaultLocalOutputTokens != want.DefaultLocalOutputTokens {
		t.Errorf("DefaultLocal* round-trip mismatch: got %+v", got)
	}
}

func TestShareableConfigExcludesLocalLLMAPIKey(t *testing.T) {
	t.Setenv("PRAIMATE_HOME", t.TempDir())
	if err := SaveConfig(&Config{
		DefaultLocalEndpoint: "https://llm.example",
		DefaultLocalAPIKey:   "must-stay-local",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := ShareableConfigJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-stay-local") || strings.Contains(string(raw), "defaultLocalApiKey") {
		t.Fatalf("shareable config leaked local API key: %s", raw)
	}
}
