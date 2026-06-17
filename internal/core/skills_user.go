package core

// User-installed skills — manual paste-in or fetched from a URL / local
// ZIP. Stored as a flat JSON file under <config>/praimate/skills.json
// so the user can hand-edit / version-control it. Each entry has the
// same shape as the built-in catalogue (Skill) plus a SourceURL for
// fetched skills (so re-imports are idempotent).
//
// Combined view (built-ins + user-installed) is exposed by
// MergedSkillCatalogue. SkillByID and ResolveSkillsPrefix both consult
// user skills BEFORE built-ins, so a user-installed skill with the
// same id overrides a built-in (intentional — lets the user replace
// a starter skill with their own version).

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// userSkillsFile is the on-disk location of the JSON catalogue.
func userSkillsFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "praimate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills.json"), nil
}

type userSkillsBody struct {
	Skills []Skill `json:"skills"`
}

var userSkillsMu sync.Mutex

// LoadUserSkills returns the user-installed skill catalogue. Empty
// slice when the file hasn't been written yet.
func LoadUserSkills() []Skill {
	path, err := userSkillsFile()
	if err != nil {
		return nil
	}
	userSkillsMu.Lock()
	defer userSkillsMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var body userSkillsBody
	if err := json.Unmarshal(b, &body); err != nil {
		return nil
	}
	out := make([]Skill, len(body.Skills))
	copy(out, body.Skills)
	return out
}

// saveUserSkills persists the slice atomically (write tempfile, rename).
func saveUserSkills(skills []Skill) error {
	path, err := userSkillsFile()
	if err != nil {
		return err
	}
	userSkillsMu.Lock()
	defer userSkillsMu.Unlock()
	body, err := json.MarshalIndent(userSkillsBody{Skills: skills}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MergedSkillCatalogue returns user-installed skills first (so they
// override built-ins on duplicate IDs), then every built-in not yet
// seen.
func MergedSkillCatalogue() []Skill {
	user := LoadUserSkills()
	seen := make(map[string]bool, len(user))
	out := make([]Skill, 0, len(user)+len(builtinSkills))
	for _, s := range user {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	for _, s := range builtinSkills {
		if seen[s.ID] {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// idSlug normalises a name into a stable kebab-case skill id.
var idSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

func idSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = idSlugRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// AddUserSkill inserts (or updates by id) a user skill. CLIs may be
// empty to mark the skill universal; the GUI surfaces these under each
// CLI tab. Returns the saved entry.
func AddUserSkill(s Skill) (*Skill, error) {
	s.Source = strings.TrimSpace(s.Source)
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.Body = strings.TrimSpace(s.Body)
	if s.Name == "" {
		return nil, errors.New("skill name is required")
	}
	if s.Body == "" {
		return nil, errors.New("skill body is empty — paste markdown or import from a URL / ZIP")
	}
	if s.ID == "" {
		s.ID = idSlug(s.Name)
	}
	if s.ID == "" {
		return nil, errors.New("skill name has no usable letters or digits")
	}
	if s.Source == "" {
		s.Source = "user"
	}
	if s.CLIs == nil {
		s.CLIs = []string{}
	}
	existing := LoadUserSkills()
	replaced := false
	for i := range existing {
		if existing[i].ID == s.ID {
			existing[i] = s
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, s)
	}
	if err := saveUserSkills(existing); err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteUserSkill removes a user skill by id. Built-ins can't be
// deleted (they're compiled in). Returns nil when the id wasn't a
// user skill — idempotent.
func DeleteUserSkill(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	existing := LoadUserSkills()
	out := existing[:0]
	for _, s := range existing {
		if s.ID == id {
			continue
		}
		out = append(out, s)
	}
	return saveUserSkills(out)
}

// ImportSkillFromURL fetches a skill body from a URL — either a single
// markdown file (text/markdown content) or a git URL / ZIP archive of
// markdown files (concatenated). Returns a Skill seeded with the
// fetched body and metadata derived from the URL; the caller fills in
// CLIs + (optional) name overrides before calling AddUserSkill.
//
// The user-supplied URL is preserved on the Skill.Source so the GUI
// can show provenance and offer a "Re-fetch" action.
func ImportSkillFromURL(ctx context.Context, rawURL string) (*Skill, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("URL is empty")
	}

	// Single markdown file (Github raw, gist raw, plain http) — easiest
	// path. Detect by extension OR by HEAD content-type.
	lower := strings.ToLower(rawURL)
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") || strings.HasSuffix(lower, ".txt") {
		body, err := fetchHTTPBody(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		return &Skill{
			ID:          idSlug(deriveSkillNameFromURL(rawURL)),
			Name:        deriveSkillNameFromURL(rawURL),
			Description: "Imported from " + rawURL,
			Body:        string(body),
			Source:      rawURL,
		}, nil
	}

	// ZIP — download, extract markdown files, concat.
	if strings.HasSuffix(lower, ".zip") {
		zipBytes, err := fetchHTTPBody(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		body, err := concatMarkdownFromZipBytes(zipBytes)
		if err != nil {
			return nil, err
		}
		return &Skill{
			ID:          idSlug(deriveSkillNameFromURL(rawURL)),
			Name:        deriveSkillNameFromURL(rawURL),
			Description: "Imported (ZIP) from " + rawURL,
			Body:        body,
			Source:      rawURL,
		}, nil
	}

	// HEAD probe — if the server returns markdown content-type, treat as
	// raw file. Otherwise refuse and tell the user to point at a .md or
	// .zip URL (avoid blindly cloning an arbitrary git repo here — we'd
	// have to figure out which file is the skill).
	ct, err := probeContentType(ctx, rawURL)
	if err == nil && (strings.Contains(ct, "markdown") || strings.HasPrefix(ct, "text/plain")) {
		body, err := fetchHTTPBody(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		return &Skill{
			ID:          idSlug(deriveSkillNameFromURL(rawURL)),
			Name:        deriveSkillNameFromURL(rawURL),
			Description: "Imported from " + rawURL,
			Body:        string(body),
			Source:      rawURL,
		}, nil
	}

	return nil, errors.New("URL must point at a .md / .markdown / .txt file OR a .zip archive of markdown files")
}

// ImportSkillFromZipFile reads a local .zip and concatenates the
// markdown files inside it. For users who downloaded a skill bundle
// out-of-band and want to import without uploading it anywhere.
func ImportSkillFromZipFile(path string) (*Skill, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("path is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	body, err := concatMarkdownFromZipBytes(b)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &Skill{
		ID:          idSlug(name),
		Name:        name,
		Description: "Imported (ZIP) from " + path,
		Body:        body,
		Source:      "file://" + path,
	}, nil
}

// fetchHTTPBody downloads the body of an HTTP(S) URL with a 30s
// timeout and a friendly User-Agent.
func fetchHTTPBody(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL scheme must be http(s)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "praimate-skills/1.0")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MB cap
}

func probeContentType(ctx context.Context, raw string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "praimate-skills/1.0")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type"), nil
}

// concatMarkdownFromZipBytes walks a zip, concatenates every .md /
// .markdown / .txt file (sorted by path), separates them with `---`.
// Skips macOS metadata, dotfiles, and anything outside the safe size
// bound.
func concatMarkdownFromZipBytes(zipBytes []byte) (string, error) {
	const maxFile = 256 << 10 // 256 KB per file
	zr, err := zip.NewReader(bytesReaderAt(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return "", fmt.Errorf("invalid zip: %w", err)
	}
	var files []*zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(filepath.Base(name), ".") || strings.Contains(name, "__macosx") {
			continue
		}
		if !(strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") || strings.HasSuffix(name, ".txt")) {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return "", errors.New("zip contains no .md / .markdown / .txt files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var b strings.Builder
	for _, f := range files {
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open %s: %w", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, int64(maxFile)))
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f.Name, err)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString("<!-- ")
		b.WriteString(f.Name)
		b.WriteString(" -->\n\n")
		b.Write(content)
	}
	return b.String(), nil
}

func deriveSkillNameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := filepath.Base(u.Path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = u.Host
	}
	return strings.ReplaceAll(base, "_", " ")
}

// bytesReaderAt is a tiny helper because we already have []byte but
// archive/zip needs io.ReaderAt. Avoids pulling in bytes.Reader's
// extra surface.
type byteReaderAt []byte

func (b byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func bytesReaderAt(b []byte) byteReaderAt { return byteReaderAt(b) }
