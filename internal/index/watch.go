package index

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"orbis/internal/vault"
)

const metaPrefix = vault.MetaDir + "/meta/"

// Watch keeps the cache fresh until ctx ends. onChange (optional) is called
// with the vault-relative paths touched in each debounced batch.
func (ix *Index) Watch(ctx context.Context, onChange func(paths []string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	addTree := func(root string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if p == filepath.Join(ix.V.Root, vault.MetaDir) {
				addMeta(w, filepath.Join(p, "meta")) // sidecars yes, index.db churn no
				return filepath.SkipDir
			}
			if p != ix.V.Root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			w.Add(p)
			return nil
		})
	}
	addTree(ix.V.Root)

	pending := map[string]bool{}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-w.Errors:
			_ = err // ponytail: dropped events are healed by the next Sync (app start / manual refresh)
		case ev := <-w.Events:
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					addTree(ev.Name)
				}
			}
			rel, err := ix.V.Rel(ev.Name)
			if err != nil || strings.HasPrefix(path.Base(rel), ".") {
				continue
			}
			if strings.HasPrefix(rel, metaPrefix) {
				rel = strings.TrimSuffix(strings.TrimPrefix(rel, metaPrefix), ".json")
			} else if strings.HasPrefix(rel, vault.MetaDir+"/") {
				continue
			}
			pending[rel] = true
			timer.Reset(150 * time.Millisecond)
		case <-timer.C:
			batch := make([]string, 0, len(pending))
			for p := range pending {
				batch = append(batch, p)
			}
			clear(pending)
			ix.apply(batch)
			if onChange != nil {
				onChange(batch)
			}
		}
	}
}

func addMeta(w *fsnotify.Watcher, root string) {
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			w.Add(p)
		}
		return nil
	})
}

// apply reindexes files that changed; anything that isn't a plain file
// (a folder created, moved or deleted) falls back to a full Sync.
func (ix *Index) apply(batch []string) {
	for _, rel := range batch {
		abs, err := ix.V.Resolve(rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		switch {
		case err == nil && info.Mode().IsRegular():
			if ix.Put(rel) != nil {
				ix.Sync()
				return
			}
		case err == nil: // directory
			ix.Sync()
			return
		default: // gone: a file we know, or a folder we don't
			if _, cerr := ix.Card(rel); cerr == nil {
				ix.Remove(rel)
			} else {
				ix.Sync()
				return
			}
		}
	}
}
