package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"orbis/internal/index"
	"orbis/internal/vault"
)

func connect(t *testing.T) (*mcp.ClientSession, *vault.Vault) { return connectWith(t, nil) }

func connectWith(t *testing.T, mem *Memory) (*mcp.ClientSession, *vault.Vault) {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := New(ix, "test", mem).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, v
}

// call returns the text result and whether the tool reported an error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestWriteSearchReadNeighbors(t *testing.T) {
	cs, v := connect(t)
	out, isErr := call(t, cs, "orbis_write", map[string]any{
		"title": "Gateway plan", "summary": "Order of work for the gateway.", "content": "# Plan\nsee [[arch]]",
		"tags": []string{"gw"}, "project": "col", "session": "s1", "created_by": "claude-code"})
	if isErr || out != `{"saved":"notes/gateway-plan.md"}` {
		t.Fatalf("write = %s", out)
	}
	call(t, cs, "orbis_write", map[string]any{"title": "arch", "type": "page", "summary": "Architecture diagram.", "content": "<h1>arch</h1>", "session": "s1"})

	out, _ = call(t, cs, "orbis_search", map[string]any{"query": "gateway"})
	var sr struct{ Results []vault.Card }
	json.Unmarshal([]byte(out), &sr)
	if len(sr.Results) != 1 || sr.Results[0].Summary != "Order of work for the gateway." || sr.Results[0].Project != "col" {
		t.Fatalf("search = %s", out)
	}

	out, _ = call(t, cs, "orbis_read", map[string]any{"path": "notes/gateway-plan.md", "max_chars": 6})
	var rd struct {
		Card      vault.Card
		Content   string
		Truncated bool
	}
	json.Unmarshal([]byte(out), &rd)
	if rd.Content != "# Plan" || !rd.Truncated || rd.Card.CreatedBy != "claude-code" {
		t.Fatalf("read = %s", out)
	}

	out, _ = call(t, cs, "orbis_neighbors", map[string]any{"path": "notes/gateway-plan.md"})
	if !strings.Contains(out, `"path":"pages/arch.html"`) {
		t.Fatalf("neighbors missing wikilinked page: %s", out)
	}

	// Overwrite by path keeps one file.
	call(t, cs, "orbis_write", map[string]any{"title": "Gateway plan", "path": "notes/gateway-plan.md", "summary": "v2", "content": "new"})
	if _, err := os.Stat(filepath.Join(v.Root, "notes", "gateway-plan-2.md")); err == nil {
		t.Fatal("overwrite created a second file")
	}
	out, _ = call(t, cs, "orbis_search", map[string]any{"query": "v2"})
	if !strings.Contains(out, "notes/gateway-plan.md") {
		t.Fatalf("overwrite not reindexed: %s", out)
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	cs, _ := connect(t)
	for name, args := range map[string]map[string]any{
		"no summary": {"title": "x", "summary": " ", "content": "x"},
		"escape":     {"title": "x", "summary": "s", "content": "x", "path": "../../evil.md"},
		"meta dir":   {"title": "x", "summary": "s", "content": "x", "path": ".orbis/index.db"},
		"pdf type":   {"title": "x", "summary": "s", "content": "x", "type": "pdf"},
	} {
		if out, isErr := call(t, cs, "orbis_write", args); !isErr {
			t.Errorf("%s: expected tool error, got %s", name, out)
		}
	}
	if out, isErr := call(t, cs, "orbis_read", map[string]any{"path": "../secret"}); !isErr {
		t.Errorf("read escape: %s", out)
	}
}

func TestImportPDF(t *testing.T) {
	cs, _ := connect(t)
	src := filepath.Join(t.TempDir(), "invoice.pdf")
	os.WriteFile(src, []byte("%PDF-1.4"), 0o644)
	out, isErr := call(t, cs, "orbis_import", map[string]any{"src_path": src, "summary": "Q3 invoice", "tags": []string{"fin"}})
	if isErr || out != `{"saved":"pdfs/invoice.pdf"}` {
		t.Fatalf("import = %s", out)
	}
	out, _ = call(t, cs, "orbis_read", map[string]any{"path": "pdfs/invoice.pdf"})
	if strings.Contains(out, `"content"`) || !strings.Contains(out, "abs_path") || !strings.Contains(out, "Q3 invoice") {
		t.Fatalf("read pdf = %s", out)
	}
}

// Success criterion 4: 10 search results ≤ 1,500 tokens (~4 chars/token).
func TestSearchTokenBudget(t *testing.T) {
	cs, _ := connect(t)
	for i := range 12 {
		call(t, cs, "orbis_write", map[string]any{
			"title":   fmt.Sprintf("Collections gateway rollout step %d", i),
			"summary": "Order of work for putting Collections behind the gateway, blocked on the FE change; covers routing, auth and rollback.",
			"content": strings.Repeat("long body text ", 500), "tags": []string{"collections", "gateway"}, "project": "col-131",
			"session": "2026-11-04-a1", "created_by": "claude-code"})
	}
	out, _ := call(t, cs, "orbis_search", map[string]any{"query": "gateway"})
	if tokens := len(out) / 4; tokens > 1500 {
		t.Fatalf("10 cards ≈ %d tokens (> 1500): %d chars", tokens, len(out))
	} else {
		t.Logf("10 cards ≈ %d tokens", tokens)
	}
}
