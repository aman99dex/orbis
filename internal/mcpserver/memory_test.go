package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecallBridge(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memories:search" || r.Header.Get("X-API-Key") != "k" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"text": "Aman prefers Go for local tools."})
	}))
	defer srv.Close()

	cs, _ := connectWith(t, &Memory{URL: srv.URL, Key: "k", User: "aman", HTTP: srv.Client()})
	out, isErr := call(t, cs, "orbis_recall", map[string]any{"query": "language preference", "namespace": "orbis"})
	if isErr || out != "Aman prefers Go for local tools." {
		t.Fatalf("recall = %q (err=%v)", out, isErr)
	}
	if got["user_id"] != "aman" || got["namespace"] != "orbis" || got["rewrite"] != false {
		t.Fatalf("request body = %v", got)
	}

	bad, _ := connectWith(t, &Memory{URL: srv.URL, Key: "wrong", User: "aman", HTTP: srv.Client()})
	if out, isErr := call(t, bad, "orbis_recall", map[string]any{"query": "x"}); !isErr {
		t.Fatalf("bad key should surface an error, got %q", out)
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
			t.Fatal("orbis_recall registered without MEMORY_URL")
		}
	}
}
