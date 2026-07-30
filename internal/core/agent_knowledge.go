package core

// Agent knowledge bases — a per-agent folder of reference documents
// under the user's config dir, optionally indexed as a graphify
// knowledge graph for RAG-style retrieval:
//
//	<config>/praimate/agents/<id>/knowledge/
//	    style-guide.md, spec.pdf, …       the documents (mode "raw")
//	    graphify-out/                     the RAG index (mode "rag")
//
// THE PATH IS THE CONTRACT: both modes live in the same folder, so the
// user can flip an existing agent between raw and rag at any time and
// every launch keeps working — only the injected guidance changes
// ("read the files" vs "query the graph").
//
// Distribution format: ".praimate-agent" — a plain zip carrying
// agent.yaml at its root plus knowledge/** (including graphify-out when
// built). Agents without knowledge keep round-tripping as bare YAML.

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git.jtsec.local/lab/PrAImate/internal/appdata"
)

// AgentDir returns the agent's on-disk root: <config>/praimate/agents/<id>/.
// The knowledge folder lives inside (AgentKnowledgeDir). When the studio
// launches its authoring assistant, this is the directory the helper CLI
// runs in — and the directory `agent.yaml` is mirrored to so the helper
// can read / edit the YAML the same way it reads the knowledge files.
func AgentDir(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("AgentDir: empty agent id")
	}
	base, err := appdata.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agents", id), nil
}

// AgentKnowledgeDir returns the knowledge folder for an agent id. The
// directory may not exist yet (callers MkdirAll on write paths).
func AgentKnowledgeDir(id string) (string, error) {
	dir, err := AgentDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "knowledge"), nil
}

// AgentRequirementsDir holds scripts included with an agent pack. Scripts are
// kept separate from knowledge so they never become RAG input.
func AgentRequirementsDir(id string) (string, error) {
	dir, err := AgentDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "requirements"), nil
}

func requirementsScriptPath(id, name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("requirements script must be a filename")
	}
	dir, err := AgentRequirementsDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// AgentRequirementsScriptPath resolves a validated script name to the
// agent-managed requirements directory.
func AgentRequirementsScriptPath(id, name string) (string, error) {
	return requirementsScriptPath(id, name)
}

// WriteAgentRequirementsScript stores a picked setup script under the
// agent-managed directory so it can be packaged on export.
func WriteAgentRequirementsScript(id, name string, body []byte) error {
	path, err := requirementsScriptPath(id, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o700)
}

// ReadAgentRequirementsScript returns a script only from the agent-managed
// requirements directory; callers cannot supply a path outside it.
func ReadAgentRequirementsScript(id, name string) ([]byte, error) {
	path, err := requirementsScriptPath(id, name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteAgentYAMLToDisk renders the agent as YAML into <AgentDir>/agent.yaml
// so the studio's helper CLI can read + edit it as a regular file. Used
// at helper-chat launch; the file is the helper's source of truth for the
// session, and SaveAgentYAML pulls back into the DB when the user clicks
// Save in the editor pane.
func WriteAgentYAMLToDisk(a *Agent) (string, error) {
	if a == nil {
		return "", fmt.Errorf("WriteAgentYAMLToDisk: nil agent")
	}
	dir, err := AgentDir(a.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body, err := MarshalAgentYAML(a)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ListAgentKnowledge returns the knowledge files (slash-relative,
// sorted), excluding the graphify-out index internals.
func ListAgentKnowledge(id string) ([]string, error) {
	dir, err := AgentKnowledgeDir(id)
	if err != nil {
		return nil, err
	}
	// Never nil: a nil slice serialises to JSON null and crashes list
	// renderers in the GUI (the "frozen knowledge panel" bug).
	out := []string{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "graphify-out" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, rerr := filepath.Rel(dir, path); rerr == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	sort.Strings(out)
	return out, err
}

// knowledgeNote builds the system-prompt addendum that points the CLI
// agent at its knowledge base. Empty when the agent has no knowledge
// mode set or the folder doesn't exist yet.
func knowledgeNote(a *Agent) string {
	if a == nil || a.Knowledge == "" {
		return ""
	}
	dir, err := AgentKnowledgeDir(a.ID)
	if err != nil {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	switch a.Knowledge {
	case "rag":
		return fmt.Sprintf("\n\nYour knowledge base lives at %q. It is indexed as a graphify "+
			"knowledge graph: prefer running `graphify query \"<your question>\"` from inside that "+
			"directory to retrieve relevant context, and fall back to reading the files directly "+
			"with your file tools when needed. Consult it before answering questions it covers.", dir)
	default: // raw
		return fmt.Sprintf("\n\nYour knowledge base lives at %q. Read the relevant files there "+
			"with your file tools before answering questions they cover.", dir)
	}
}

// AgentSystemPrompt is the instructions every launch surface should
// send: the agent's instructions plus the knowledge-base pointer.
func AgentSystemPrompt(a *Agent) string {
	if a == nil {
		return ""
	}
	return a.Instructions + knowledgeNote(a)
}

// AddAgentKnowledgeFiles copies files (or whole directories) into the
// agent's knowledge folder. Directory sources are copied recursively,
// preserving their internal layout under their base name.
func AddAgentKnowledgeFiles(id string, sources []string) (added int, err error) {
	dir, err := AgentKnowledgeDir(id)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	for _, src := range sources {
		fi, err := os.Stat(src)
		if err != nil {
			return added, fmt.Errorf("stat %s: %w", src, err)
		}
		if fi.IsDir() {
			base := filepath.Base(src)
			err = filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
				if werr != nil || d.IsDir() {
					return werr
				}
				if strings.HasPrefix(d.Name(), ".") {
					return nil
				}
				rel, rerr := filepath.Rel(src, path)
				if rerr != nil {
					return rerr
				}
				if cerr := copyKnowledgeFile(path, filepath.Join(dir, base, rel)); cerr != nil {
					return cerr
				}
				added++
				return nil
			})
			if err != nil {
				return added, err
			}
			continue
		}
		if err := copyKnowledgeFile(src, filepath.Join(dir, filepath.Base(src))); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func copyKnowledgeFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// DeleteAgentKnowledgeFile removes one knowledge file (rel as returned
// by ListAgentKnowledge). Path-restricted to the knowledge dir.
func DeleteAgentKnowledgeFile(id, rel string) error {
	dir, err := AgentKnowledgeDir(id)
	if err != nil {
		return err
	}
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(dir, abs); err != nil || strings.HasPrefix(r, "..") {
		return fmt.Errorf("path %q is outside the knowledge folder", rel)
	}
	return os.Remove(abs)
}

// AgentPackExt is the distribution extension for agents with knowledge.
const AgentPackExt = ".praimate-agent"

// ExportAgentPack writes a .praimate-agent zip (agent.yaml + the whole
// knowledge folder, graphify-out index included so RAG agents arrive
// pre-indexed). Works for knowledge-less agents too — the zip then
// just carries agent.yaml.
func (c *Core) ExportAgentPack(ctx context.Context, id, path string) error {
	agent, err := c.GetAgent(ctx, id)
	if err != nil {
		return err
	}
	raw, err := MarshalAgentYAML(agent)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("agent.yaml")
	if err == nil {
		_, err = w.Write(raw)
	}
	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		return fmt.Errorf("write agent.yaml: %w", err)
	}

	dir, err := AgentKnowledgeDir(id)
	if err == nil {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(dir, p)
			if rerr != nil {
				return nil
			}
			zf, zerr := zw.Create("knowledge/" + filepath.ToSlash(rel))
			if zerr != nil {
				return zerr
			}
			in, oerr := os.Open(p)
			if oerr != nil {
				return oerr
			}
			defer in.Close()
			_, cerr := io.Copy(zf, in)
			return cerr
		})
	}
	requirementsDir, err := AgentRequirementsDir(id)
	if err == nil {
		if walkErr := filepath.WalkDir(requirementsDir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(requirementsDir, p)
			if rerr != nil {
				return nil
			}
			zf, zerr := zw.Create("requirements/" + filepath.ToSlash(rel))
			if zerr != nil {
				return zerr
			}
			in, oerr := os.Open(p)
			if oerr != nil {
				return oerr
			}
			defer in.Close()
			_, cerr := io.Copy(zf, in)
			return cerr
		}); walkErr != nil && !os.IsNotExist(walkErr) {
			_ = zw.Close()
			_ = f.Close()
			return fmt.Errorf("write requirements pack files: %w", walkErr)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ImportAgentPack imports a .praimate-agent zip. The pack is fully parsed and
// extracted into a sibling staging directory before either the database or the
// live agent folders are touched. Once validation succeeds, knowledge and
// requirements are swapped with rollback protection and the agent is upserted.
func (c *Core) ImportAgentPack(ctx context.Context, path string) (*Agent, error) {
	if c.store == nil {
		return nil, fmt.Errorf("ImportAgentPack: no store configured")
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open pack: %w", err)
	}
	defer zr.Close()

	var yamlEntry *zip.File
	for _, zf := range zr.File {
		if zf.Name == "agent.yaml" {
			yamlEntry = zf
			break
		}
	}
	if yamlEntry == nil {
		return nil, fmt.Errorf("%s has no agent.yaml at its root", filepath.Base(path))
	}
	yr, err := yamlEntry.Open()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(yr, 1<<20))
	_ = yr.Close()
	if err != nil {
		return nil, err
	}
	agent, err := ParseAgentYAML(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	agentDir, err := AgentDir(agent.ID)
	if err != nil {
		return nil, err
	}
	parentDir := filepath.Dir(agentDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, err
	}
	stageDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(agentDir)+"-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageDir)

	// Extract everything before replacing live data. A malformed entry, CRC
	// failure, or short read therefore leaves the existing agent untouched.
	for _, zf := range zr.File {
		root, rel, ok := strings.Cut(zf.Name, "/")
		if !ok || (root != "knowledge" && root != "requirements") || rel == "" || strings.HasSuffix(zf.Name, "/") {
			continue
		}
		targetDir := filepath.Join(stageDir, root)
		dst := filepath.Join(targetDir, filepath.FromSlash(rel))
		if r, rerr := filepath.Rel(targetDir, dst); rerr != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("pack entry %q escapes the %s folder", zf.Name, root)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		in, oerr := zf.Open()
		if oerr != nil {
			return nil, oerr
		}
		mode := os.FileMode(0o644)
		if root == "requirements" {
			mode = 0o700
		}
		out, cerr := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if cerr != nil {
			_ = in.Close()
			return nil, cerr
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
	}

	type swap struct {
		live   string
		staged string
		backup string
		hadOld bool
		moved  bool
	}
	swaps := []swap{
		{
			live:   filepath.Join(agentDir, "knowledge"),
			staged: filepath.Join(stageDir, "knowledge"),
			backup: filepath.Join(stageDir, ".backup-knowledge"),
		},
		{
			live:   filepath.Join(agentDir, "requirements"),
			staged: filepath.Join(stageDir, "requirements"),
			backup: filepath.Join(stageDir, ".backup-requirements"),
		},
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return nil, err
	}
	rollback := func() {
		for i := len(swaps) - 1; i >= 0; i-- {
			s := &swaps[i]
			if s.moved {
				_ = os.RemoveAll(s.live)
			}
			if s.hadOld {
				_ = os.Rename(s.backup, s.live)
			}
		}
	}
	for i := range swaps {
		s := &swaps[i]
		if _, statErr := os.Stat(s.live); statErr == nil {
			if err := os.Rename(s.live, s.backup); err != nil {
				rollback()
				return nil, err
			}
			s.hadOld = true
		} else if !os.IsNotExist(statErr) {
			rollback()
			return nil, statErr
		}
		if _, statErr := os.Stat(s.staged); statErr == nil {
			if err := os.Rename(s.staged, s.live); err != nil {
				rollback()
				return nil, err
			}
			s.moved = true
		} else if !os.IsNotExist(statErr) {
			rollback()
			return nil, statErr
		}
	}

	stored, err := c.upsertAgent(ctx, agent)
	if err != nil {
		rollback()
		return nil, err
	}
	return stored, nil
}

// ImportAgentAuto imports either format by extension: .praimate-agent
// (or .zip) packs, anything else as bare YAML.
func (c *Core) ImportAgentAuto(ctx context.Context, path string) (*Agent, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case AgentPackExt, ".zip":
		return c.ImportAgentPack(ctx, path)
	default:
		return c.ImportAgent(ctx, path)
	}
}
