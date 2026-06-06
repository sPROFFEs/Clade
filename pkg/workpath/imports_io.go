package workpath

// Low-level helpers for read/modify/write on the `imports:` array in a
// workpath.json file. Used by the TUI's settings screen (Bundles
// section) so toggling a bundle on/off doesn't require hand-editing
// JSON. Preserves any non-`imports` fields verbatim via a raw-message
// map round-trip so future additions to the schema aren't dropped on
// the floor.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// rawManifest preserves every field as a json.RawMessage so we can
// round-trip a workpath.json that has fields this Go binary doesn't
// know about (e.g. a future schema addition). Only the field we care
// about is decoded/encoded explicitly.
type rawManifest map[string]json.RawMessage

// ReadImports returns the imports array from a workpath.json file.
// Returns nil (no error) when the file is missing or has no `imports`
// field — both are valid initial states.
func ReadImports(path string) ([]string, error) {
	raw, err := readRawManifest(path)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	rawImp, ok := raw["imports"]
	if !ok {
		return nil, nil
	}
	var imports []string
	if err := json.Unmarshal(rawImp, &imports); err != nil {
		return nil, fmt.Errorf("parse imports: %w", err)
	}
	return imports, nil
}

// AddImport ensures importRef is present in the imports array,
// creating the file or array if needed. Idempotent: re-adding an
// existing entry is a no-op and returns nil. importRef is compared
// verbatim (no path normalization) — callers pass the exact string
// the manifest should record.
func AddImport(path, importRef string) error {
	raw, err := readRawManifest(path)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = rawManifest{}
	}
	imports, err := decodeImports(raw)
	if err != nil {
		return err
	}
	for _, x := range imports {
		if x == importRef {
			return nil
		}
	}
	imports = append(imports, importRef)
	return writeImports(path, raw, imports)
}

// RemoveImport drops importRef from the imports array (if present),
// preserving every other field. When the resulting array is empty
// the `imports` field is removed entirely so the manifest stays
// minimal. Idempotent: removing an absent entry is a no-op.
func RemoveImport(path, importRef string) error {
	raw, err := readRawManifest(path)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	imports, err := decodeImports(raw)
	if err != nil {
		return err
	}
	out := imports[:0]
	dropped := false
	for _, x := range imports {
		if x == importRef {
			dropped = true
			continue
		}
		out = append(out, x)
	}
	if !dropped {
		return nil
	}
	if len(out) == 0 {
		delete(raw, "imports")
		return writeRawManifest(path, raw)
	}
	return writeImports(path, raw, out)
}

func decodeImports(raw rawManifest) ([]string, error) {
	rawImp, ok := raw["imports"]
	if !ok {
		return nil, nil
	}
	var imports []string
	if err := json.Unmarshal(rawImp, &imports); err != nil {
		return nil, fmt.Errorf("parse imports: %w", err)
	}
	return imports, nil
}

func writeImports(path string, raw rawManifest, imports []string) error {
	encoded, err := json.Marshal(imports)
	if err != nil {
		return err
	}
	raw["imports"] = encoded
	return writeRawManifest(path, raw)
}

// readRawManifest reads a workpath.json file into the field-preserving
// map. Returns (nil, nil) when the file doesn't exist — the caller
// treats that as "empty manifest" so toggling a bundle on into a
// pristine workpath.json works.
func readRawManifest(path string) (rawManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(body) == 0 {
		return rawManifest{}, nil
	}
	var raw rawManifest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return raw, nil
}

// writeRawManifest re-marshals raw with 2-space indent (matching the
// rest of the project's workpath.json convention) and writes via a
// temp file + rename to keep the operation atomic.
func writeRawManifest(path string, raw rawManifest) error {
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".clade-tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
