package portable

import (
	"archive/zip"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"orbis/internal/index"
	"orbis/internal/vault"
)

func hashes(t *testing.T, v *vault.Vault) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	v.Walk(func(rel string, _ fs.FileInfo) error {
		b, _ := os.ReadFile(filepath.Join(v.Root, rel))
		out[rel] = sha256.Sum256(b)
		return nil
	})
	return out
}

// Success criterion 6: export → import elsewhere → identical files, search works.
func TestRoundTrip(t *testing.T) {
	a, _ := vault.Open(t.TempDir())
	a.Write(vault.Card{Path: "notes/n.md", Summary: "portable note", Tags: []string{"x"}}, []byte("hello [[p]]"))
	a.Write(vault.Card{Path: "pdfs/p.pdf", Summary: "a pdf card"}, []byte("%PDF-1.4 binary\x00\x01"))
	os.MkdirAll(filepath.Join(a.Root, "images", "deep"), 0o755)
	os.WriteFile(filepath.Join(a.Root, "images", "deep", "i.png"), []byte{0x89, 'P', 'N', 'G'}, 0o644)
	ixA, _ := index.Open(a)
	ixA.Sync()
	ixA.Close()

	zipPath := filepath.Join(t.TempDir(), "orbis.zip")
	n, err := Export(a, zipPath)
	if err != nil || n != 4 { // 3 files + 1 pdf sidecar
		t.Fatalf("Export = %d, %v", n, err)
	}
	zr, _ := zip.OpenReader(zipPath)
	for _, f := range zr.File {
		if filepath.Base(f.Name) == "index.db" {
			t.Fatal("index.db leaked into the zip")
		}
	}
	zr.Close()

	b, _ := vault.Open(t.TempDir())
	res, err := Import(b, zipPath)
	if err != nil || res.Added != 4 {
		t.Fatalf("Import = %+v, %v", res, err)
	}
	ha, hb := hashes(t, a), hashes(t, b)
	if len(ha) != len(hb) {
		t.Fatalf("file count %d vs %d", len(ha), len(hb))
	}
	for k, h := range ha {
		if hb[k] != h {
			t.Errorf("%s differs after round trip", k)
		}
	}
	ixB, _ := index.Open(b)
	defer ixB.Close()
	ixB.Sync()
	if c, err := ixB.Card("pdfs/p.pdf"); err != nil || c.Summary != "a pdf card" {
		t.Fatalf("pdf card lost: %+v %v", c, err)
	}
	if got, _ := ixB.Search(index.Query{Text: "portable"}); len(got) != 1 {
		t.Fatalf("search after import = %v", got)
	}

	// Importing again changes nothing; a locally edited file is kept and the incoming copy renamed.
	os.WriteFile(filepath.Join(b.Root, "notes", "n.md"), []byte("local edit"), 0o644)
	res, _ = Import(b, zipPath)
	if res.Unchanged != 3 || len(res.Renamed) != 1 || res.Renamed[0] != "notes/n (imported).md" {
		t.Fatalf("re-import = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(b.Root, "notes", "n.md")); string(got) != "local edit" {
		t.Fatal("local edit overwritten")
	}
}

func TestImportRejectsZipSlip(t *testing.T) {
	for _, bad := range []string{"../evil.txt", "notes/../../evil.txt", ".orbis/index.db", "/abs.txt", ".orbis/meta/../../x"} {
		zipPath := filepath.Join(t.TempDir(), "bad.zip")
		f, _ := os.Create(zipPath)
		zw := zip.NewWriter(f)
		w, _ := zw.Create(bad)
		w.Write([]byte("x"))
		zw.Close()
		f.Close()
		v, _ := vault.Open(t.TempDir())
		if _, err := Import(v, zipPath); err == nil {
			t.Errorf("zip entry %q was accepted", bad)
		}
	}
}
