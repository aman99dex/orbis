package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioSmoke builds the real binary and talks to `orbis mcp` over stdio,
// the way Claude Code does.
func TestStdioSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := os.Getenv("ORBIS_BIN") // e.g. the wails-built GUI .exe, to prove stdio works for it too
	if bin == "" {
		bin = filepath.Join(t.TempDir(), "orbis.exe")
		if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	}
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "smoke"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin, "mcp", "-root", t.TempDir())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if !strings.Contains(cs.InitializeResult().Instructions, "orbis_write") {
		t.Fatal("server instructions missing")
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "orbis_write", Arguments: map[string]any{
		"title": "smoke", "summary": "stdio smoke note", "content": "hello"}}); err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "orbis_search", Arguments: map[string]any{"query": "smoke"}})
	if err != nil {
		t.Fatal(err)
	}
	if txt := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(txt, "notes/smoke.md") {
		t.Fatalf("search over stdio = %s", txt)
	}
}
