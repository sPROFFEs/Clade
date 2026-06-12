package core

// Agent knowledge bases — a per-agent folder of reference documents
// under the user's config dir, optionally indexed as a graphify
// knowledge graph for RAG-style retrieval:
//
//	<config>/praimate/agents/<id>/knowledge/
//	    style-guide.md, spec.pdf, …       the documents (mode "raw")
//	    .graphify/                        the RAG index (mode "rag")
//
// THE PATH IS THE CONTRACT: both modes live in the same folder, so the
// user can flip an existing agent between raw and rag at any time and
// every launch keeps working — only the injected guidance changes
// ("read the files" vs "query the graph").
//
// Distribution format: ".praimate-agent" — a plain zip carrying
// agent.yaml at its root plus knowledge/** (including .graphify when
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
)

// AgentKnowledgeDir returns the knowledge folder for an agent id. The
// directory may not exist yet (callers MkdirAll on write paths).
func AgentKnowledgeDir(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("AgentKnowledgeDir: empty agent id")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "praimate", "agents", id, "knowledge"), nil
}

// ListAgentKnowledge returns the knowledge files (slash-relative,
// sorted), excluding the .graphify index internals.
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
			if d.Name() == ".graphify" {
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
// knowledge folder, .graphify index included so RAG agents arrive
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
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ImportAgentPack imports a .praimate-agent zip: validates and upserts
// agent.yaml, then REPLACES the agent's knowledge folder with the
// pack's knowledge/** contents (zip-slip guarded).
func (c *Core) ImportAgentPack(ctx context.Context, path string) (*Agent, error) {
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
	agent, err := c.ImportAgentYAML(ctx, body, "")
	if err != nil {
		return nil, err
	}

	dir, err := AgentKnowledgeDir(agent.ID)
	if err != nil {
		return agent, err
	}
	// Replace, don't merge — the pack is the source of truth.
	if err := os.RemoveAll(dir); err != nil {
		return agent, err
	}
	for _, zf := range zr.File {
		rel, ok := strings.CutPrefix(zf.Name, "knowledge/")
		if !ok || rel == "" || strings.HasSuffix(zf.Name, "/") {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if r, rerr := filepath.Rel(dir, dst); rerr != nil || strings.HasPrefix(r, "..") {
			return agent, fmt.Errorf("pack entry %q escapes the knowledge folder", zf.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return agent, err
		}
		in, oerr := zf.Open()
		if oerr != nil {
			return agent, oerr
		}
		out, cerr := os.Create(dst)
		if cerr != nil {
			_ = in.Close()
			return agent, cerr
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return agent, err
		}
	}
	return agent, nil
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
