// Package mcpserver exposes the vault to coding agents over MCP.
// Results are compact JSON text (no duplicate structured copy) to keep token cost low.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"orbis/internal/index"
	"orbis/internal/vault"
)

const instructions = `Orbis is the user's local context store (notes, HTML pages, artifacts, PDFs, images).
- Look before you read: orbis_search / orbis_list / orbis_neighbors return short cards; call orbis_read only for the few you need.
- When you produce something worth keeping (plans, findings, reports, decisions, HTML artifacts), save it with orbis_write.
- Always give a 1-3 sentence summary that would let a future agent decide whether to open the file without reading it.
- Reuse the same session id for everything you write in one task, and set project when the work belongs to one.
- To update an existing file, pass its path to orbis_write.`

// New builds the server. version is reported to clients; mem may be nil.
func New(ix *index.Index, version string, mem *Memory) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "orbis", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	h := handlers{ix: ix, v: ix.V}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{Name: "orbis_search", Annotations: ro,
		Description: "Full-text search over titles, summaries, tags and content. Returns cards (path, type, title, summary, tags), best match first."}, h.search)
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_list", Annotations: ro,
		Description: "List cards, most recently updated first. Filter by type, tag or project."}, h.list)
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_neighbors", Annotations: ro,
		Description: "Cards connected to a file: links in/out, and files sharing a tag, project or session. depth 1-3."}, h.neighbors)
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_read", Annotations: ro,
		Description: "Read one file: its card plus text content. For PDFs/images returns the card and abs_path so you can open the file with your own tools."}, h.read)
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_write",
		Description: "Create or update a text file (note=markdown, page/artifact=html or any text) with its card. Omit path to create a new file named from the title; pass an existing path to overwrite it."}, h.write)
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_import",
		Description: "Copy a local file (PDF, image, anything) into Orbis with a card. It lands in the folder for its type."}, h.importFile)
	if mem != nil {
		mem.register(s)
	}
	return s
}

type handlers struct {
	ix *index.Index
	v  *vault.Vault
}

type searchIn struct {
	Query   string `json:"query" jsonschema:"words to search for"`
	Type    string `json:"type,omitempty" jsonschema:"note | page | artifact | pdf | image | file"`
	Tag     string `json:"tag,omitempty"`
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results, default 10, max 100"`
}

type listIn struct {
	Type    string `json:"type,omitempty" jsonschema:"note | page | artifact | pdf | image | file"`
	Tag     string `json:"tag,omitempty"`
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results, default 10, max 100"`
}

type neighborsIn struct {
	Path  string `json:"path" jsonschema:"vault-relative path, e.g. notes/plan.md"`
	Depth int    `json:"depth,omitempty" jsonschema:"hops, 1-3, default 1"`
}

type readIn struct {
	Path     string `json:"path" jsonschema:"vault-relative path, e.g. notes/plan.md"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"truncate content to this many characters, default 20000"`
}

type writeIn struct {
	Title     string   `json:"title"`
	Summary   string   `json:"summary" jsonschema:"1-3 sentences: what this is and when it is useful"`
	Content   string   `json:"content" jsonschema:"full file content (markdown for notes, html for pages)"`
	Type      string   `json:"type,omitempty" jsonschema:"note (default) | page | artifact | file; ignored when path is given"`
	Path      string   `json:"path,omitempty" jsonschema:"existing vault-relative path to overwrite, or a new one; omit to auto-name"`
	Tags      []string `json:"tags,omitempty"`
	Project   string   `json:"project,omitempty"`
	Links     []string `json:"links,omitempty" jsonschema:"vault-relative paths this file relates to"`
	Session   string   `json:"session,omitempty" jsonschema:"same id for everything written in one task"`
	CreatedBy string   `json:"created_by,omitempty" jsonschema:"your name, e.g. claude-code"`
}

type importIn struct {
	SrcPath string   `json:"src_path" jsonschema:"absolute path of the file to copy in"`
	Summary string   `json:"summary" jsonschema:"1-3 sentences: what this is and when it is useful"`
	Title   string   `json:"title,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Project string   `json:"project,omitempty"`
	Session string   `json:"session,omitempty"`
}

func (h handlers) search(_ context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
	cs, err := h.ix.Search(index.Query{Text: in.Query, Type: in.Type, Tag: in.Tag, Project: in.Project, Limit: in.Limit})
	return reply(map[string]any{"results": nonNil(cs)}, err)
}

func (h handlers) list(_ context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, any, error) {
	cs, err := h.ix.List(index.Query{Type: in.Type, Tag: in.Tag, Project: in.Project, Limit: in.Limit})
	return reply(map[string]any{"results": nonNil(cs)}, err)
}

func (h handlers) neighbors(_ context.Context, _ *mcp.CallToolRequest, in neighborsIn) (*mcp.CallToolResult, any, error) {
	cs, es, err := h.ix.Neighbors(in.Path, in.Depth)
	return reply(map[string]any{"cards": cs, "edges": es}, err)
}

func (h handlers) read(_ context.Context, _ *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, any, error) {
	c, body, err := h.v.Read(in.Path)
	if err != nil {
		return reply(nil, err)
	}
	abs, _ := h.v.Resolve(in.Path)
	max := in.MaxChars
	if max <= 0 {
		max = 20000
	}
	truncated := false
	if r := []rune(body); len(r) > max {
		body, truncated = string(r[:max]), true
	}
	out := map[string]any{"card": c, "abs_path": abs}
	if vault.IsText(in.Path) {
		out["content"] = body
		out["truncated"] = truncated
	}
	return reply(out, nil)
}

func (h handlers) write(_ context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Summary) == "" {
		return reply(nil, fmt.Errorf("summary is required: 1-3 sentences a future agent can triage from"))
	}
	rel := in.Path
	if rel == "" {
		typ := in.Type
		if typ == "" {
			typ = "note"
		}
		dir, ok := vault.TypeDirs[typ]
		if !ok || typ == "pdf" || typ == "image" {
			return reply(nil, fmt.Errorf("type %q: use note, page, artifact or file (orbis_import for PDFs/images)", typ))
		}
		rel = h.v.FreePath(dir + "/" + vault.Slug(in.Title) + vault.DefaultExt(typ))
	} else if path.Ext(rel) == "" {
		return reply(nil, fmt.Errorf("path %q needs a file extension", rel))
	}
	createdBy := in.CreatedBy
	if createdBy == "" {
		createdBy = "agent"
	}
	c, err := h.v.Write(vault.Card{Path: rel, Title: in.Title, Summary: in.Summary, Tags: in.Tags,
		Project: in.Project, Links: in.Links, Session: in.Session, CreatedBy: createdBy}, []byte(in.Content))
	if err != nil {
		return reply(nil, err)
	}
	if err := h.ix.Put(c.Path); err != nil {
		return reply(nil, fmt.Errorf("saved %s but indexing failed: %w", c.Path, err))
	}
	return reply(map[string]any{"saved": c.Path}, nil)
}

func (h handlers) importFile(_ context.Context, _ *mcp.CallToolRequest, in importIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Summary) == "" {
		return reply(nil, fmt.Errorf("summary is required"))
	}
	c, err := h.v.Import(in.SrcPath, vault.Card{Title: in.Title, Summary: in.Summary, Tags: in.Tags,
		Project: in.Project, Session: in.Session, CreatedBy: "agent"})
	if err != nil {
		return reply(nil, err)
	}
	if err := h.ix.Put(c.Path); err != nil {
		return reply(nil, fmt.Errorf("imported %s but indexing failed: %w", c.Path, err))
	}
	return reply(map[string]any{"saved": c.Path}, nil)
}

// reply returns compact JSON text, or a tool error the agent can read and fix.
func reply(v any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

func nonNil(cs []vault.Card) []vault.Card {
	if cs == nil {
		return []vault.Card{}
	}
	return cs
}
