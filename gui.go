package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"orbis/internal/index"
	"orbis/internal/mcpserver"
	"orbis/internal/portable"
	"orbis/internal/vault"
)

//go:embed all:frontend/dist
var assets embed.FS

// App is bound to the frontend; every exported method is callable from JS.
type App struct {
	ctx context.Context
	ix  *index.Index
	v   *vault.Vault
	mem *mcpserver.Memory // nil when Agentic Memory is not set up
}

func runGUI(root string) error {
	ix, err := open(root, false)
	if err != nil {
		return err
	}
	defer ix.Close()
	app := &App{ix: ix, v: ix.V, mem: mcpserver.MemoryFromEnv()}
	return wails.Run(&options.App{
		Title:     "Orbis",
		Width:     1320,
		Height:    840,
		MinWidth:  720,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Middleware (not Handler) so /vault/ wins in both dev and production builds.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/vault/") {
						app.serveVault(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 18, G: 24, B: 41, A: 255},
		OnStartup: func(ctx context.Context) {
			app.ctx = ctx
			go ix.Watch(ctx, func(paths []string) { runtime.EventsEmit(ctx, "vault:changed", paths) })
			go func() { // index in the background; the UI refreshes when it lands
				if n, err := ix.Sync(); err != nil {
					log.Printf("initial sync: %v", err)
				} else if n > 0 {
					runtime.EventsEmit(ctx, "vault:changed", []string{})
				}
				debug.FreeOSMemory() // indexing reads every file once; hand that heap back
			}()
		},
		Bind: []any{app},
	})
}

// serveVault serves /vault/<rel> so PDFs, images and HTML pages render in the webview.
// HTML gets a CSP sandbox so pages run in an opaque origin and can't reach the app bridge.
func (a *App) serveVault(w http.ResponseWriter, r *http.Request) {
	rel, ok := strings.CutPrefix(r.URL.Path, "/vault/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	abs, err := a.v.Resolve(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".html", ".htm", ".svg":
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-popups allow-forms allow-modals")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, abs)
}

// Doc is what the viewer needs for one file.
type Doc struct {
	Card    vault.Card `json:"card"`
	Content string     `json:"content"` // raw file text, frontmatter included; empty for binaries
	Text    bool       `json:"text"`
	Size    int64      `json:"size"`
}

func (a *App) Root() string { return a.v.Root }

func (a *App) Cards() ([]vault.Card, error) { return a.ix.All() }

func (a *App) Search(q string) ([]vault.Card, error) {
	return a.ix.Search(index.Query{Text: q, Limit: 50})
}

func (a *App) Open(rel string) (Doc, error) {
	c, _, err := a.v.Read(rel)
	if err != nil {
		return Doc{}, err
	}
	abs, _ := a.v.Resolve(rel)
	info, err := os.Stat(abs)
	if err != nil {
		return Doc{}, err
	}
	d := Doc{Card: c, Text: vault.IsText(rel), Size: info.Size()}
	if d.Text && info.Size() <= 8<<20 {
		raw, err := os.ReadFile(abs)
		if err != nil {
			return Doc{}, err
		}
		d.Content = string(raw)
	}
	return d, nil
}

// SaveContent writes the editor buffer exactly as typed.
func (a *App) SaveContent(rel, content string) error {
	if err := a.v.WriteRaw(rel, []byte(content)); err != nil {
		return err
	}
	return a.ix.Put(rel)
}

// SaveCard updates title/summary/tags/project without touching the body.
func (a *App) SaveCard(c vault.Card) error {
	if err := a.v.SaveCard(c); err != nil {
		return err
	}
	return a.ix.Put(c.Path)
}

// Create makes a new note or page and returns its path.
func (a *App) Create(typ, title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = "Untitled " + time.Now().Format("2006-01-02 15:04")
	}
	dir, ok := vault.TypeDirs[typ]
	if !ok || typ == "pdf" || typ == "image" {
		return "", fmt.Errorf("can't create %q here; use Import for PDFs and images", typ)
	}
	rel := a.v.FreePath(dir + "/" + vault.Slug(title) + vault.DefaultExt(typ))
	body := "# " + title + "\n\n"
	if vault.DefaultExt(typ) == ".html" {
		body = "<!doctype html>\n<html>\n<head><meta charset=\"utf-8\"><title>" + title + "</title></head>\n<body>\n<h1>" + title + "</h1>\n</body>\n</html>\n"
	}
	c, err := a.v.Write(vault.Card{Path: rel, Title: title, CreatedBy: "human"}, []byte(body))
	if err != nil {
		return "", err
	}
	return c.Path, a.ix.Put(c.Path)
}

// Delete removes a file after the user confirms in a native dialog.
func (a *App) Delete(rel string) (bool, error) {
	ans, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "Delete file",
		Message: "Delete " + rel + "? This can't be undone.", Buttons: []string{"Delete", "Cancel"},
		DefaultButton: "Cancel", CancelButton: "Cancel",
	})
	if err != nil || (ans != "Delete" && ans != "Yes") {
		return false, err
	}
	if err := a.v.Delete(rel); err != nil {
		return false, err
	}
	return true, a.ix.Remove(rel)
}

// Import lets the user pick files to copy in; returns the new paths.
func (a *App) Import() ([]string, error) {
	srcs, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add files to Orbis"})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range srcs {
		c, err := a.v.Import(s, vault.Card{CreatedBy: "human"})
		if err != nil {
			return out, err
		}
		if err := a.ix.Put(c.Path); err != nil {
			log.Printf("index %s: %v", c.Path, err)
		}
		out = append(out, c.Path)
	}
	return out, nil
}

type Hood struct {
	Cards []vault.Card `json:"cards"`
	Edges []index.Edge `json:"edges"`
}

func (a *App) Neighbors(rel string, depth int) (Hood, error) {
	cs, es, err := a.ix.Neighbors(rel, depth)
	return Hood{cs, es}, err
}

type GraphData struct {
	Nodes []index.GNode `json:"nodes"`
	Edges []index.Edge  `json:"edges"`
}

func (a *App) Graph() (GraphData, error) {
	ns, es, err := a.ix.Graph()
	return GraphData{ns, es}, err
}

// OpenExternal opens a URL in the system browser (links clicked inside pages).
func (a *App) OpenExternal(url string) {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		runtime.BrowserOpenURL(a.ctx, url)
	}
}

// AgentCommand is the one-liner that connects Claude Code to this Orbis.
func (a *App) AgentCommand() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "orbis"
	}
	return fmt.Sprintf(`claude mcp add orbis --scope user -- "%s" mcp -root "%s"`, exe, a.v.Root)
}

// ExportZip saves the whole Orbis as one zip; returns the path or "" if cancelled.
func (a *App) ExportZip() (string, error) {
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Export Orbis", DefaultFilename: "orbis-" + time.Now().Format("2006-01-02") + ".zip",
		Filters: []runtime.FileFilter{{DisplayName: "Zip archive", Pattern: "*.zip"}},
	})
	if err != nil || dest == "" {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
		dest += ".zip"
	}
	_, err = portable.Export(a.v, dest)
	return dest, err
}

// ImportZip merges an exported zip into this Orbis.
func (a *App) ImportZip() (*portable.Result, error) {
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import an Orbis zip", Filters: []runtime.FileFilter{{DisplayName: "Zip archive", Pattern: "*.zip"}},
	})
	if err != nil || src == "" {
		return nil, err
	}
	res, err := portable.Import(a.v, src)
	if err != nil {
		return nil, err
	}
	_, err = a.ix.Sync()
	return &res, err
}

// MemoryMode is "local", "server", or "" when Agentic Memory isn't set up.
func (a *App) MemoryMode() string {
	if a.mem == nil {
		return ""
	}
	return a.mem.Mode()
}

// MemoryContext is the briefing a new agent session would get (no project selected).
func (a *App) MemoryContext() (string, error) {
	if a.mem == nil {
		return "", fmt.Errorf("Agentic Memory isn't set up")
	}
	return a.mem.Context(a.ctx, "")
}

// Recall searches memories across every project (an empty project means all of them).
func (a *App) Recall(q string) (string, error) {
	if a.mem == nil {
		return "", fmt.Errorf("Agentic Memory isn't set up")
	}
	return a.mem.Recall(a.ctx, q, "", 0, false)
}
