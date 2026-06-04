package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestToolCatalog_GraphifyShape(t *testing.T) {
	for _, o := range []OS{OSMacOS, OSLinux, OSWSL, OSWindows} {
		for _, act := range []Action{ActionInstall, ActionUpdate} {
			got := allToolMethods(ToolGraphify, act, o)
			if len(got) != 1 {
				t.Fatalf("graphify %s/%s: got %d methods, want exactly 1", o, act, len(got))
			}
			m := got[0]
			if m.ID != "uv" {
				t.Errorf("graphify %s/%s: ID=%q, want \"uv\"", o, act, m.ID)
			}
			if m.ManagedPrefix != "graphify" {
				t.Errorf("graphify %s/%s: ManagedPrefix=%q", o, act, m.ManagedPrefix)
			}
			if m.ManagedPrefixPkg == "" {
				t.Errorf("graphify %s/%s: ManagedPrefixPkg unset", o, act)
			}
			// Supply-chain pin: explicit index URL.
			if !strings.Contains(m.Command, "--index-url=https://pypi.org/simple/") {
				t.Errorf("graphify %s/%s: missing --index-url in command %q", o, act, m.Command)
			}
			// Project-local: must NOT be a `-g` style install for graphify.
			if strings.Contains(m.Command, " -g ") {
				t.Errorf("graphify %s/%s: should not use -g, got %q", o, act, m.Command)
			}
		}
	}
}

func TestManagedToolPrefix_PathShape(t *testing.T) {
	prefix, err := ManagedToolPrefix("graphify")
	if err != nil {
		t.Fatalf("ManagedToolPrefix: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(prefix), "clade/tools/graphify") {
		t.Errorf("prefix = %q, want it to end with clade/tools/graphify", prefix)
	}
	binDir, err := ManagedToolBinDir("graphify")
	if err != nil {
		t.Fatalf("ManagedToolBinDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(binDir), "clade/tools/graphify/bin") {
		t.Errorf("binDir = %q, want .../clade/tools/graphify/bin", binDir)
	}
}

func TestKnownTools_GraphifyPresent(t *testing.T) {
	got := KnownTools()
	if len(got) == 0 {
		t.Fatal("KnownTools returned empty catalog")
	}
	found := false
	for _, x := range got {
		if x.ID == ToolGraphify {
			found = true
			if x.Binary != "graphify" {
				t.Errorf("graphify Binary = %q, want \"graphify\"", x.Binary)
			}
		}
	}
	if !found {
		t.Error("ToolGraphify not in KnownTools catalog")
	}
}
