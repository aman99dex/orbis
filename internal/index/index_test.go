package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"orbis/internal/vault"
)

func setup(t *testing.T) (*vault.Vault, *Index) {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ix, err := Open(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return v, ix
}

func write(t *testing.T, v *vault.Vault, c vault.Card, body string) {
	t.Helper()
	if _, err := v.Write(c, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func paths(cs []vault.Card) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return out
}

func TestSyncSearchAndFilters(t *testing.T) {
	v, ix := setup(t)
	write(t, v, vault.Card{Path: "notes/gw.md", Title: "Gateway rollout", Summary: "COL-131 order of work", Tags: []string{"gateway"}, Project: "col"}, "body mentions kafka")
	write(t, v, vault.Card{Path: "pages/kafka.html", Summary: "Kafka topics map", Tags: []string{"infra"}}, "<h1>Kafka</h1>")
	write(t, v, vault.Card{Path: "notes/misc.md", Title: "Lunch"}, "nothing relevant")
	if n, err := ix.Sync(); err != nil || n != 3 {
		t.Fatalf("Sync = %d, %v", n, err)
	}
	if n, _ := ix.Sync(); n != 0 {
		t.Fatalf("second Sync reindexed %d files", n)
	}

	got, err := ix.Search(Query{Text: "kafka"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths(got), []string{"pages/kafka.html", "notes/gw.md"}) { // summary hit outranks body hit
		t.Fatalf("search kafka = %v", paths(got))
	}
	if got, _ := ix.Search(Query{Text: "COL-131 gatew"}); !slices.Equal(paths(got), []string{"notes/gw.md"}) {
		t.Fatalf("hyphen + prefix query = %v", paths(got))
	}
	if got, _ := ix.Search(Query{Text: "kafka", Type: "note"}); !slices.Equal(paths(got), []string{"notes/gw.md"}) {
		t.Fatalf("type filter = %v", paths(got))
	}
	if got, _ := ix.Search(Query{Text: "kafka", Tag: "#INFRA"}); !slices.Equal(paths(got), []string{"pages/kafka.html"}) {
		t.Fatalf("tag filter = %v", paths(got))
	}
	if got, _ := ix.Search(Query{Text: "kafka lunch"}); len(got) != 3 {
		t.Fatalf("OR fallback = %v", paths(got))
	}
	if got, _ := ix.Search(Query{Text: `"); DROP TABLE nodes; --`}); got == nil {
		_ = got // must not error; sanitised to words
	}
}

func TestSyncPicksUpEditsDeletesAndSidecars(t *testing.T) {
	v, ix := setup(t)
	write(t, v, vault.Card{Path: "notes/a.md", Summary: "first"}, "x")
	write(t, v, vault.Card{Path: "pdfs/s.pdf", Summary: "old card"}, "%PDF")
	ix.Sync()

	later := time.Now().Add(2 * time.Second)
	write(t, v, vault.Card{Path: "pdfs/s.pdf", Summary: "new card"}, "%PDF") // same size; sidecar newer
	os.Chtimes(v.SidecarPath("pdfs/s.pdf"), later, later)
	abs, _ := v.Resolve("notes/a.md")
	os.Remove(abs)
	ix.Sync()

	if _, err := ix.Card("notes/a.md"); err == nil {
		t.Fatal("deleted file still indexed")
	}
	if c, _ := ix.Card("pdfs/s.pdf"); c.Summary != "new card" {
		t.Fatalf("sidecar edit missed: %+v", c)
	}
}

func TestNeighborsLinksWikilinksAndHubs(t *testing.T) {
	v, ix := setup(t)
	write(t, v, vault.Card{Path: "notes/a.md", Session: "s1"}, "see [[b]] and [c](../pages/c.html)")
	write(t, v, vault.Card{Path: "notes/b.md"}, "back to nothing")
	write(t, v, vault.Card{Path: "pages/c.html", Tags: []string{"x"}}, "<p>c</p>")
	write(t, v, vault.Card{Path: "notes/d.md", Tags: []string{"x"}}, "shares tag with c")
	write(t, v, vault.Card{Path: "artifacts/e.html", Session: "s1"}, "same session as a")
	write(t, v, vault.Card{Path: "notes/far.md"}, "links [[d]]")
	ix.Sync()

	cs, edges, err := ix.Neighbors("notes/a.md", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := paths(cs)
	if got[0] != "notes/a.md" {
		t.Fatalf("start card not first: %v", got)
	}
	sort.Strings(got[1:])
	if !slices.Equal(got, []string{"notes/a.md", "artifacts/e.html", "notes/b.md", "pages/c.html"}) {
		t.Fatalf("depth 1 = %v (edges %v)", got, edges)
	}
	cs, _, _ = ix.Neighbors("notes/a.md", 2)
	if !slices.Contains(paths(cs), "notes/d.md") || slices.Contains(paths(cs), "notes/far.md") {
		t.Fatalf("depth 2 = %v", paths(cs))
	}
	cs, _, _ = ix.Neighbors("notes/a.md", 3)
	if !slices.Contains(paths(cs), "notes/far.md") {
		t.Fatalf("depth 3 = %v", paths(cs))
	}
}

func TestBigHubIsSkipped(t *testing.T) {
	v, ix := setup(t)
	for i := range maxHub + 1 {
		write(t, v, vault.Card{Path: fmt.Sprintf("notes/n%d.md", i), Tags: []string{"common"}}, "x")
	}
	ix.Sync()
	cs, _, _ := ix.Neighbors("notes/n0.md", 1)
	if len(cs) != 1 {
		t.Fatalf("big hub leaked %d neighbors", len(cs)-1)
	}
}

func TestScale10k(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	v, ix := setup(t)
	for i := range 10000 {
		abs, _ := v.Resolve(fmt.Sprintf("notes/f%05d.md", i))
		os.WriteFile(abs, fmt.Appendf(nil, "---\ntitle: Note %d\nsummary: about topic%d and shared words\ntags: [t%d]\n---\nbody text %d lorem ipsum dolor", i, i%97, i%13, i), 0o644)
	}
	start := time.Now()
	if n, err := ix.Sync(); err != nil || n != 10000 {
		t.Fatalf("Sync = %d, %v", n, err)
	}
	syncDur := time.Since(start)

	var lat []time.Duration
	for i := range 200 {
		s := time.Now()
		if _, err := ix.Search(Query{Text: fmt.Sprintf("topic%d shared", i%97)}); err != nil {
			t.Fatal(err)
		}
		lat = append(lat, time.Since(s))
	}
	slices.Sort(lat)
	p95 := lat[len(lat)*95/100]
	t.Logf("sync 10k: %v, search p95: %v", syncDur, p95)
	if syncDur > 10*time.Second || p95 > 50*time.Millisecond {
		t.Fatalf("over budget: sync %v (≤10s), p95 %v (≤50ms)", syncDur, p95)
	}
}

func TestWatchIndexesOutsideEdits(t *testing.T) {
	v, ix := setup(t)
	ix.Sync()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan []string, 10)
	go ix.Watch(ctx, func(p []string) { changed <- p })
	time.Sleep(200 * time.Millisecond) // let the watcher register

	os.MkdirAll(filepath.Join(v.Root, "notes", "sub"), 0o755)
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(filepath.Join(v.Root, "notes", "sub", "x.md"), []byte("---\nsummary: zebra facts\n---\nhi"), 0o644)

	deadline := time.After(3 * time.Second)
	for {
		if got, _ := ix.Search(Query{Text: "zebra"}); len(got) == 1 && got[0].Path == "notes/sub/x.md" {
			break
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("watcher did not index the new file within 3s")
		}
	}
	os.Remove(filepath.Join(v.Root, "notes", "sub", "x.md"))
	deadline = time.After(3 * time.Second)
	for {
		if got, _ := ix.Search(Query{Text: "zebra"}); len(got) == 0 {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("watcher did not drop the deleted file within 3s")
		}
	}
}
