package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Memory is the optional bridge to Agentic Memory. Orbis works fully without it.
//
// Two modes:
//   - server: MEMORY_URL is set → POST /memories:search and /memories:context.
//   - local:  the `agentic-memory` CLI (on PATH, or ORBIS_MEMORY_CMD) reads its own
//     SQLite store directly; no server to run.
type Memory struct {
	URL, Key, User string   // server mode
	Cmd            []string // local mode: argv prefix, e.g. ["agentic-memory"]
	HTTP           *http.Client
}

// MemoryFromEnv picks the mode from the environment, or returns nil when
// neither a server nor the CLI is available (orbis_recall stays unregistered).
//
//	MEMORY_URL, MEMORY_API_KEY (or API_KEY), MEMORY_USER   → server mode
//	ORBIS_MEMORY_CMD="uv run --directory C:\src\Agentic_Memory agentic-memory" → local mode
//	`agentic-memory` on PATH                                → local mode
func MemoryFromEnv() *Memory {
	if u := os.Getenv("MEMORY_URL"); u != "" {
		key := os.Getenv("MEMORY_API_KEY")
		if key == "" {
			key = os.Getenv("API_KEY")
		}
		who := os.Getenv("MEMORY_USER")
		if who == "" {
			who = "default"
			if cu, err := user.Current(); err == nil {
				who = cu.Username
			}
		}
		return &Memory{URL: strings.TrimRight(u, "/"), Key: key, User: who, HTTP: &http.Client{Timeout: 30 * time.Second}}
	}
	if c := os.Getenv("ORBIS_MEMORY_CMD"); c != "" {
		if argv := SplitCommand(c); len(argv) > 0 {
			return &Memory{Cmd: argv}
		}
	}
	if p, err := exec.LookPath("agentic-memory"); err == nil {
		return &Memory{Cmd: []string{p}}
	}
	return nil
}

// Mode is "server" or "local".
func (m *Memory) Mode() string {
	if m.URL != "" {
		return "server"
	}
	return "local"
}

// Recall searches memories. project is a folder in local mode (empty = user-wide
// only) or a namespace in server mode.
func (m *Memory) Recall(ctx context.Context, query, project string, budget int, rewrite bool) (string, error) {
	if m.URL == "" {
		return m.run(ctx, "recall", query, "--project", project)
	}
	if budget <= 0 {
		budget = 1500
	}
	body := map[string]any{"user_id": m.User, "query": query, "budget": budget, "rewrite": rewrite}
	if project != "" {
		body["namespace"] = project
	}
	return m.post(ctx, "/memories:search", body)
}

// Context is what a new session would be told: profile, project brief, recent sessions.
func (m *Memory) Context(ctx context.Context, project string) (string, error) {
	if m.URL == "" {
		return m.run(ctx, "context", "--project", project)
	}
	body := map[string]any{"user_id": m.User, "budget": 1200}
	if project != "" {
		body["namespace"] = project
	}
	return m.post(ctx, "/memories:context", body)
}

func (m *Memory) post(ctx context.Context, route string, body map[string]any) (string, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+route, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", m.Key)
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("memory service unreachable at %s: %w", m.URL, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("memory service returned %s: %s", resp.Status, bytes.TrimSpace(b))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("memory service sent unexpected JSON: %w", err)
	}
	return orNothing(out.Text), nil
}

// run calls the local CLI. Its first run may download an embedding model, hence the long timeout.
func (m *Memory) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.Cmd[0], append(append([]string{}, m.Cmd[1:]...), args...)...)
	// Python on Windows writes pipes in the ANSI code page unless told otherwise.
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 600 {
			msg = "…" + msg[len(msg)-600:]
		}
		return "", fmt.Errorf("agentic-memory %s failed: %v: %s", args[0], err, msg)
	}
	return orNothing(strings.TrimSpace(stdout.String())), nil
}

func orNothing(s string) string {
	switch strings.TrimSpace(s) {
	case "", "Nothing found.", "Nothing remembered yet.":
		return "Nothing remembered for that yet."
	}
	return s
}

// SplitCommand splits a command line on spaces, keeping "double quoted" parts together.
func SplitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, have = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

type recallIn struct {
	Query   string `json:"query" jsonschema:"what you want to remember about the user or past work"`
	Project string `json:"project,omitempty" jsonschema:"local: project folder (default: the folder you run in); server: namespace"`
	Budget  int    `json:"budget,omitempty" jsonschema:"server mode: max tokens of recalled text, default 1500"`
	Rewrite bool   `json:"rewrite,omitempty" jsonschema:"server mode: let the memory service rewrite the query with its LLM"`
}

func (m *Memory) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_recall", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		Description: "Recall facts and past sessions from the user's Agentic Memory. Complements orbis_search (files) with remembered facts."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, any, error) {
			project := in.Project
			if project == "" && m.URL == "" {
				project, _ = os.Getwd() // the agent starts us in its project folder
			}
			text, err := m.Recall(ctx, in.Query, project, in.Budget, in.Rewrite)
			if err != nil {
				return reply(nil, err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
}
