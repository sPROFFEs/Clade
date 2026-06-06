package workpath

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestAddImport_CreatesArrayIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpath.json")
	_ = os.WriteFile(path, []byte(`{"description":"hi","version":"1"}`+"\n"), 0o644)

	if err := AddImport(path, "_common/graphify"); err != nil {
		t.Fatalf("AddImport: %v", err)
	}
	got, err := ReadImports(path)
	if err != nil {
		t.Fatalf("ReadImports: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"_common/graphify"}) {
		t.Errorf("imports = %v, want [_common/graphify]", got)
	}
	// Other fields preserved.
	raw, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["description"] != "hi" || m["version"] != "1" {
		t.Errorf("other fields lost: %v", m)
	}
}

func TestAddImport_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpath.json")
	_ = os.WriteFile(path, []byte(`{"imports":["_common/graphify"]}`+"\n"), 0o644)

	if err := AddImport(path, "_common/graphify"); err != nil {
		t.Fatalf("AddImport: %v", err)
	}
	got, _ := ReadImports(path)
	if len(got) != 1 {
		t.Errorf("re-adding existing entry should be no-op; got %v", got)
	}
}

func TestRemoveImport_DropsField_WhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpath.json")
	_ = os.WriteFile(path, []byte(`{"description":"x","imports":["_common/graphify"]}`+"\n"), 0o644)

	if err := RemoveImport(path, "_common/graphify"); err != nil {
		t.Fatalf("RemoveImport: %v", err)
	}
	// imports field gone, other fields kept.
	body, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if _, present := m["imports"]; present {
		t.Errorf("imports should be removed entirely when last entry was dropped; got %v", m)
	}
	if m["description"] != "x" {
		t.Errorf("description should survive; got %v", m)
	}
}

func TestRemoveImport_KeepsOtherEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpath.json")
	_ = os.WriteFile(path, []byte(`{"imports":["_common/foo","_common/graphify","_common/bar"]}`+"\n"), 0o644)

	if err := RemoveImport(path, "_common/graphify"); err != nil {
		t.Fatalf("RemoveImport: %v", err)
	}
	got, _ := ReadImports(path)
	sort.Strings(got)
	want := []string{"_common/bar", "_common/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("imports = %v, want %v", got, want)
	}
}

func TestRemoveImport_AbsentIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workpath.json")
	_ = os.WriteFile(path, []byte(`{"description":"x"}`+"\n"), 0o644)
	if err := RemoveImport(path, "_common/graphify"); err != nil {
		t.Errorf("removing absent entry should be a no-op; got err %v", err)
	}
}

func TestReadImports_MissingFileIsNoErr(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadImports(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Errorf("missing file should not error; got %v", err)
	}
	if got != nil {
		t.Errorf("missing file should return nil imports; got %v", got)
	}
}
