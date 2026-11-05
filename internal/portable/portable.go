// Package portable moves a whole Orbis between machines as one zip.
// The zip holds the content folders plus .orbis/meta (cards); the SQLite
// index is left out and rebuilt on the other side.
package portable

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"orbis/internal/vault"
)

// maxImport caps total uncompressed bytes so a hostile zip can't fill the disk.
// ponytail: fixed 8 GiB; make it a flag if real vaults get bigger.
const maxImport = 8 << 30

// Export writes every content file and card under v.Root into dest.
func Export(v *vault.Vault, dest string) (int, error) {
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	zw := zip.NewWriter(f)
	n := 0
	add := func(abs, name string, info fs.FileInfo) error {
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name, h.Method = name, zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		src, err := os.Open(abs)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		n++
		return err
	}
	err = v.Walk(func(rel string, info fs.FileInfo) error {
		return add(filepath.Join(v.Root, filepath.FromSlash(rel)), rel, info)
	})
	if err == nil {
		meta := filepath.Join(v.Root, vault.MetaDir, "meta")
		err = filepath.WalkDir(meta, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".orbis-tmp-") {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(v.Root, p)
			return add(p, filepath.ToSlash(rel), info)
		})
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dest)
	}
	return n, err
}

// Result says what Import did.
type Result struct {
	Added     int      `json:"added"`
	Unchanged int      `json:"unchanged"`
	Renamed   []string `json:"renamed"` // conflicts saved next to the existing file
}

// Import merges a zip into v. Existing files are never overwritten: an
// identical file is skipped, a different one is saved as "name (imported).ext".
// Caller should run index Sync afterwards.
func Import(v *vault.Vault, src string) (Result, error) {
	var res Result
	zr, err := zip.OpenReader(src)
	if err != nil {
		return res, err
	}
	defer zr.Close()
	var total uint64
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		total += zf.UncompressedSize64
		if total > maxImport {
			return res, fmt.Errorf("zip expands to more than %d GiB; refusing", maxImport>>30)
		}
		name := zf.Name
		meta := strings.HasPrefix(name, vault.MetaDir+"/meta/")
		if !meta {
			if _, err := v.Resolve(name); err != nil { // rejects ../, absolute paths and .orbis/index.db
				return res, fmt.Errorf("unsafe path in zip: %q", name)
			}
		} else if strings.Contains(name, "..") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
			return res, fmt.Errorf("unsafe path in zip: %q", name)
		}
		data, err := readAll(zf)
		if err != nil {
			return res, fmt.Errorf("%s: %w", name, err)
		}
		target := filepath.Join(v.Root, filepath.FromSlash(name))
		existing, err := os.ReadFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return res, err
		case bytes.Equal(existing, data):
			res.Unchanged++
			continue
		case meta:
			continue // keep the local card; the renamed file below gets its own
		default:
			ext := path.Ext(name)
			name = v.FreePath(strings.TrimSuffix(name, ext) + " (imported)" + ext)
			target = filepath.Join(v.Root, filepath.FromSlash(name))
			res.Renamed = append(res.Renamed, name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return res, err
		}
		os.Chtimes(target, zf.Modified, zf.Modified)
		res.Added++
	}
	return res, nil
}

func readAll(zf *zip.File) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// Read one byte past the declared size to catch lying headers.
	b, err := io.ReadAll(io.LimitReader(rc, int64(zf.UncompressedSize64)+1))
	if err == nil && uint64(len(b)) > zf.UncompressedSize64 {
		err = errors.New("entry larger than its header says")
	}
	return b, err
}
