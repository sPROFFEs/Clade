package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDBPathMigratesLegacyLinuxDatabaseFiles(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("the split ~/.praimate layout was Linux-only")
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	root := filepath.Join(base, "config", "praimate")
	legacy := filepath.Join(home, ".praimate", "db.sqlite")
	t.Setenv("HOME", home)
	t.Setenv("PRAIMATE_HOME", root)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy+".key", []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := DefaultDBPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "db.sqlite")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	for _, suffix := range []string{"", ".key"} {
		if _, err := os.Stat(want + suffix); err != nil {
			t.Fatalf("migrated file %s missing: %v", suffix, err)
		}
		if _, err := os.Stat(legacy + suffix); !os.IsNotExist(err) {
			t.Fatalf("legacy file %s remains: %v", suffix, err)
		}
	}
}
