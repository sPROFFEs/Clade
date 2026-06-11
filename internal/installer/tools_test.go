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

func TestToolCatalog_GstackShape(t *testing.T) {
	got := allToolMethods(ToolGstack, ActionInstall, OSLinux)
	if len(got) != 1 {
		t.Fatalf("gstack got %d methods, want exactly 1", len(got))
	}
	m := got[0]
	if m.ID != "bash" {
		t.Errorf("gstack ID=%q, want bash", m.ID)
	}
	if m.ManagedPrefix != "gstack" {
		t.Errorf("gstack ManagedPrefix=%q, want gstack", m.ManagedPrefix)
	}
	if !strings.Contains(m.ManagedPrefixPkg, "github.com/garrytan/gstack") {
		t.Errorf("gstack ManagedPrefixPkg=%q, want upstream repo URL", m.ManagedPrefixPkg)
	}
	for _, want := range []string{"GSTACK_SKIP_FONTS=1", "GSTACK_SKIP_COREUTILS=1", "--host auto"} {
		if !strings.Contains(m.Command, want) {
			t.Errorf("gstack command missing %q: %s", want, m.Command)
		}
	}
	for _, want := range []string{"git", "bun"} {
		if !containsString(m.Prereqs, want) {
			t.Errorf("gstack prereqs = %v, want %s", m.Prereqs, want)
		}
	}
}

func TestToolCatalog_ScrapeGraphShape(t *testing.T) {
	got := allToolMethods(ToolScrapeGraph, ActionInstall, OSLinux)
	if len(got) != 1 {
		t.Fatalf("scrapegraph got %d methods, want exactly 1", len(got))
	}
	m := got[0]
	if m.ID != "uv" {
		t.Errorf("scrapegraph ID=%q, want uv", m.ID)
	}
	if m.ManagedPrefix != "scrapegraph" {
		t.Errorf("scrapegraph ManagedPrefix=%q, want scrapegraph", m.ManagedPrefix)
	}
	for _, want := range []string{"scrapegraphai", "scrapegraph-py"} {
		if !strings.Contains(m.ManagedPrefixPkg, want) {
			t.Errorf("scrapegraph ManagedPrefixPkg=%q, want %s", m.ManagedPrefixPkg, want)
		}
	}
	if !strings.Contains(m.Command, "--index-url=https://pypi.org/simple/") {
		t.Errorf("scrapegraph command missing pinned index URL: %s", m.Command)
	}
	if !containsString(m.Prereqs, "uv") {
		t.Errorf("scrapegraph prereqs = %v, want uv", m.Prereqs)
	}
}

func TestManagedToolPrefix_PathShape(t *testing.T) {
	prefix, err := ManagedToolPrefix("graphify")
	if err != nil {
		t.Fatalf("ManagedToolPrefix: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(prefix), "praimate/tools/graphify") {
		t.Errorf("prefix = %q, want it to end with praimate/tools/graphify", prefix)
	}
	binDir, err := ManagedToolBinDir("graphify")
	if err != nil {
		t.Fatalf("ManagedToolBinDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(binDir), "praimate/tools/graphify/bin") {
		t.Errorf("binDir = %q, want .../praimate/tools/graphify/bin", binDir)
	}
}

func TestKnownTools_AllExpectedPresent(t *testing.T) {
	got := KnownTools()
	if len(got) == 0 {
		t.Fatal("KnownTools returned empty catalog")
	}
	want := map[ToolID]string{
		ToolGraphify:    "graphify",
		ToolGstack:      "gstack",
		ToolScrapeGraph: "scrapegraph-search",
	}
	for _, x := range got {
		bin, ok := want[x.ID]
		if !ok {
			continue
		}
		if x.Binary != bin {
			t.Errorf("%s Binary = %q, want %q", x.ID, x.Binary, bin)
		}
		delete(want, x.ID)
	}
	for id := range want {
		t.Errorf("%s not in KnownTools catalog", id)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestPraimateCodeMethods_PresentPerOS(t *testing.T) {
	// Linux/macOS → a curl download method; Windows → powershell.
	unix := allToolMethods(ToolPraimateCode, ActionInstall, OSLinux)
	if len(unix) != 1 || unix[0].ID != "curl" {
		t.Fatalf("linux praimate-code methods = %+v", unix)
	}
	if !strings.Contains(unix[0].Command, "praimate-code-") {
		t.Fatalf("download command missing asset url: %q", unix[0].Command)
	}
	win := allToolMethods(ToolPraimateCode, ActionInstall, OSWindows)
	if len(win) != 1 || win[0].ID != "powershell" {
		t.Fatalf("windows praimate-code methods = %+v", win)
	}
}

func TestKnownTools_ExcludesPraimateCode(t *testing.T) {
	// PrAImate Code is a CLI surfaced in the CLIs browser, not a
	// companion tool — it must NOT appear in the Tools catalog.
	for _, tl := range KnownTools() {
		if tl.ID == ToolPraimateCode {
			t.Fatal("praimate-code should not be in KnownTools (it's a CLI)")
		}
	}
}
