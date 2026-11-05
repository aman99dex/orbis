package vault

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func open(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestResolveRejectsEscapes(t *testing.T) {
	v := open(t)
	for _, bad := range []string{"", "../x", "notes/../../x", `..\x`, "/etc/passwd", `C:\x`, "C:x", ".orbis/index.db", ".orbis", "notes/../.orbis/meta/a"} {
		if _, err := v.Resolve(bad); err == nil {
			t.Errorf("Resolve(%q) should fail", bad)
		}
	}
	got, err := v.Resolve("notes/a/b.md")
	if err != nil || got != filepath.Join(v.Root, "notes", "a", "b.md") {
		t.Fatalf("Resolve good path = %q, %v", got, err)
	}
}

func TestWriteReadMarkdownCard(t *testing.T) {
	v := open(t)
	in := Card{Path: "notes/gw.md", Title: "Gateway", Summary: "Order of work", Tags: []string{"col-131"}, Project: "col", CreatedBy: "claude-code", Session: "s1"}
	body := "---\ntitle: stale\n---\n# Gateway\nSee [[arch]] and [plan](../pages/plan.html) and https://x.com.\n"
	if _, err := v.Write(in, []byte(body)); err != nil {
		t.Fatal(err)
	}
	c, text, err := v.Read("notes/gw.md")
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Gateway" || c.Summary != "Order of work" || c.Project != "col" || c.Session != "s1" || c.Type != "note" {
		t.Fatalf("card = %+v", c)
	}
	if strings.Contains(text, "stale") || !strings.HasPrefix(text, "# Gateway") {
		t.Fatalf("body = %q", text)
	}
	if !slices.Equal(c.Links, []string{"[[arch]]", "pages/plan.html"}) {
		t.Fatalf("links = %v", c.Links)
	}
}

func TestSidecarForNonMarkdown(t *testing.T) {
	v := open(t)
	_, err := v.Write(Card{Path: "pages/r.html", Summary: "Q3 report", Tags: []string{"fin"}},
		[]byte(`<html><title>Report</title><body><a href="/notes/gw.md">gw</a></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := v.Read("pages/r.html")
	if c.Title != "Report" || c.Summary != "Q3 report" || c.Type != "page" || !slices.Equal(c.Links, []string{"notes/gw.md"}) {
		t.Fatalf("card = %+v", c)
	}
	if err := v.Delete("pages/r.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.SidecarPath("pages/r.html")); !os.IsNotExist(err) {
		t.Fatal("sidecar not removed")
	}
}

func TestFallbacksWithoutCard(t *testing.T) {
	v := open(t)
	abs, _ := v.Resolve("notes/raw.md")
	os.WriteFile(abs, []byte("# Hello\nsome text here"), 0o644)
	c, _, err := v.Read("notes/raw.md")
	if err != nil || c.Title != "Hello" || c.Summary != "# Hello some text here" {
		t.Fatalf("card = %+v, %v", c, err)
	}
}

func TestImportAndFreePath(t *testing.T) {
	v := open(t)
	src := filepath.Join(t.TempDir(), "Scan.PDF")
	os.WriteFile(src, []byte("%PDF-1.4"), 0o644)
	a, err := v.Import(src, Card{Summary: "scan"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := v.Import(src, Card{Summary: "scan again"})
	if a.Path != "pdfs/Scan.PDF" || b.Path != "pdfs/Scan-2.PDF" || a.Type != "pdf" || a.Summary != "scan" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"COL-131: Gateway Plan!": "col-131-gateway-plan", "  ": "untitled", "Ünïcode": "n-code"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
