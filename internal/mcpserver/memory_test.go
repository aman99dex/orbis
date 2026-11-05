package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestRecallServerMode(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			http.Error(w, "bad or missing X-API-Key", http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		switch r.URL.Path {
		case "/memories:search":
			json.NewEncoder(w).Encode(map[string]any{"text": "Aman prefers Go for local tools."})
		case "/memories:context":
			json.NewEncoder(w).Encode(map[string]any{"text": "Profile: builds Orbis."})
		}
	}))
	defer srv.Close()
	mem := &Memory{URL: srv.URL, Key: "k", User: "aman", HTTP: srv.Client()}

	cs, _ := connectWith(t, mem)
	out, isErr := call(t, cs, "orbis_recall", map[string]any{"query": "language preference", "project": "orbis"})
	if isErr || out != "Aman prefers Go for local tools." {
		t.Fatalf("recall = %q (err=%v)", out, isErr)
	}
	if got["user_id"] != "aman" || got["namespace"] != "orbis" || got["rewrite"] != false {
		t.Fatalf("request body = %v", got)
	}
	got = nil
	if txt, err := mem.Context(t.Context(), ""); err != nil || txt != "Profile: builds Orbis." || got["namespace"] != nil {
		t.Fatalf("context = %q %v body=%v", txt, err, got)
	}
	bad, _ := connectWith(t, &Memory{URL: srv.URL, Key: "wrong", User: "aman", HTTP: srv.Client()})
	if out, isErr := call(t, bad, "orbis_recall", map[string]any{"query": "x"}); !isErr || !strings.Contains(out, "401") {
		t.Fatalf("bad key should surface a 401, got %q", out)
	}
}

// TestFakeAgenticMemory stands in for the CLI: the test binary re-runs itself in this mode.
func TestFakeAgenticMemory(t *testing.T) {
	if os.Getenv("ORBIS_FAKE_AM") != "1" {
		t.Skip("helper process")
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if args[0] == "fail" {
		fmt.Fprintln(os.Stderr, "Traceback: ANTHROPIC_API_KEY missing")
		os.Exit(3)
	}
	fmt.Printf("%s|%s", strings.Join(args, "|"), os.Getenv("PYTHONIOENCODING")) // echo what we were given
	os.Exit(0)
}

func TestRecallLocalMode(t *testing.T) {
	t.Setenv("ORBIS_FAKE_AM", "1")
	mem := &Memory{Cmd: []string{os.Args[0], "-test.run=^TestFakeAgenticMemory$", "--"}}
	cs, _ := connectWith(t, mem)
	cwd, _ := os.Getwd()
	out, isErr := call(t, cs, "orbis_recall", map[string]any{"query": "what ü did we decide"})
	if want := "recall|what ü did we decide|--project|" + cwd + "|utf-8"; isErr || out != want {
		t.Fatalf("local recall = %q, want %q", out, want)
	}
	if txt, _ := mem.Context(t.Context(), ""); txt != "context|--project||utf-8" {
		t.Fatalf("local context = %q", txt)
	}
	failing := &Memory{Cmd: []string{os.Args[0], "-test.run=^TestFakeAgenticMemory$", "--", "fail"}}
	if _, err := failing.Recall(t.Context(), "x", "", 0, false); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY missing") {
		t.Fatalf("CLI failure should carry stderr, got %v", err)
	}
}

func TestMemoryFromEnv(t *testing.T) {
	t.Setenv("MEMORY_URL", "")
	t.Setenv("ORBIS_MEMORY_CMD", `uv run --directory "C:\My Projects\Agentic_Memory" agentic-memory`)
	m := MemoryFromEnv()
	if m == nil || m.Mode() != "local" || !slices.Equal(m.Cmd, []string{"uv", "run", "--directory", `C:\My Projects\Agentic_Memory`, "agentic-memory"}) {
		t.Fatalf("ORBIS_MEMORY_CMD → %+v", m)
	}
	t.Setenv("MEMORY_URL", "http://127.0.0.1:8000/")
	if m := MemoryFromEnv(); m == nil || m.Mode() != "server" || m.URL != "http://127.0.0.1:8000" {
		t.Fatalf("MEMORY_URL → %+v", m)
	}
}

func TestRecallHiddenWithoutConfig(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "orbis_recall" {
			t.Fatal("orbis_recall registered without memory configured")
		}
	}
}
