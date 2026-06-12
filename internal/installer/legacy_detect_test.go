package installer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Tools installed under the LEGACY clade prefix (pre-rebrand) must
// still detect — "graphify installed but not detected".
func TestDetectTools_FindsLegacyCladePrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-script fake binary")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("PATH", "/usr/bin:/bin")

	bin := filepath.Join(cfg, "clade", "tools", "graphify", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho graphify 9.9.9\n"
	if err := os.WriteFile(filepath.Join(bin, "graphify"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tool := range DetectTools(context.Background()) {
		if tool.ID == ToolGraphify {
			if !tool.Available {
				t.Fatalf("graphify in legacy clade prefix not detected (probeErr=%s)", tool.ProbeError)
			}
			if tool.Version != "graphify 9.9.9" {
				t.Errorf("version = %q", tool.Version)
			}
			return
		}
	}
	t.Fatal("graphify not in tool list")
}
