package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Memory is the optional bridge to an Agentic_Memory service
// (POST /memories:search). Orbis works fully without it.
type Memory struct {
	URL, Key, User string
	HTTP           *http.Client
}

// MemoryFromEnv reads the same variables Agentic_Memory's own client uses:
// MEMORY_URL, MEMORY_API_KEY (or API_KEY), and MEMORY_USER (default: OS user).
// Returns nil when MEMORY_URL is unset, which leaves orbis_recall unregistered.
func MemoryFromEnv() *Memory {
	u := os.Getenv("MEMORY_URL")
	if u == "" {
		return nil
	}
	key := os.Getenv("MEMORY_API_KEY")
	if key == "" {
		key = os.Getenv("API_KEY")
	}
	who := os.Getenv("MEMORY_USER")
	if who == "" {
		if cu, err := user.Current(); err == nil {
			who = cu.Username
		} else {
			who = "default"
		}
	}
	return &Memory{URL: u, Key: key, User: who, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type recallIn struct {
	Query     string `json:"query" jsonschema:"what you want to remember about the user or past work"`
	Namespace string `json:"namespace,omitempty" jsonschema:"project scope, if the memory service uses one"`
	Budget    int    `json:"budget,omitempty" jsonschema:"max tokens of recalled text, default 1500"`
	Rewrite   bool   `json:"rewrite,omitempty" jsonschema:"let the memory service rewrite the query with its LLM (slower, smarter)"`
}

func (m *Memory) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "orbis_recall", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		Description: "Recall facts and past conversation from the user's Agentic Memory service. Complements orbis_search (files) with remembered facts."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, any, error) {
			text, err := m.recall(ctx, in)
			if err != nil {
				return reply(nil, err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
}

func (m *Memory) recall(ctx context.Context, in recallIn) (string, error) {
	budget := in.Budget
	if budget <= 0 {
		budget = 1500
	}
	body := map[string]any{"user_id": m.User, "query": in.Query, "budget": budget, "rewrite": in.Rewrite}
	if in.Namespace != "" {
		body["namespace"] = in.Namespace
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+"/memories:search", bytes.NewReader(raw))
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
	if out.Text == "" {
		return "Nothing remembered for that query.", nil
	}
	return out.Text, nil
}
