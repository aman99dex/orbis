// Package vault owns the Orbis folder: layout, path safety, and node cards.
// Files on disk are the source of truth; everything else is a cache.
package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// TypeDirs maps a node type to its top-level folder.
var TypeDirs = map[string]string{
	"note":     "notes",
	"page":     "pages",
	"artifact": "artifacts",
	"pdf":      "pdfs",
	"image":    "images",
	"file":     "files",
}

// MetaDir holds sidecar cards and the index cache.
const MetaDir = ".orbis"

// Card is the cheap summary of a file that agents read before the file itself.
type Card struct {
	Path      string    `json:"path" yaml:"-"`
	Type      string    `json:"type" yaml:"-"`
	Title     string    `json:"title" yaml:"title,omitempty"`
	Summary   string    `json:"summary" yaml:"summary,omitempty"`
	Tags      []string  `json:"tags,omitempty" yaml:"tags,omitempty"`
	Project   string    `json:"project,omitempty" yaml:"project,omitempty"`
	Links     []string  `json:"links,omitempty" yaml:"links,omitempty"`
	CreatedBy string    `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	Session   string    `json:"session,omitempty" yaml:"session,omitempty"`
	Updated   time.Time `json:"updated" yaml:"-"`
}

type Vault struct{ Root string }

// Open creates the layout under root if missing.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, d := range append(dirs(), MetaDir, filepath.Join(MetaDir, "meta")) {
		if err := os.MkdirAll(filepath.Join(abs, d), 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return &Vault{Root: abs}, nil
}

func dirs() []string {
	out := make([]string, 0, len(TypeDirs))
	for _, d := range TypeDirs {
		out = append(out, d)
	}
	return out
}

// Resolve returns the absolute path for a vault-relative path ("notes/a.md"),
// or an error if it escapes the vault or points into the meta dir.
func (v *Vault) Resolve(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.ContainsRune(rel, ':') {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	// Backslash is a separator on every OS here: a vault zipped on macOS must
	// not carry a "..\x" name that escapes when opened on Windows.
	slash := strings.ReplaceAll(rel, `\`, "/")
	if strings.HasPrefix(slash, "/") || slices.Contains(strings.Split(slash, "/"), "..") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	clean := path.Clean("/" + slash)[1:]
	if clean == "" || clean == MetaDir || strings.HasPrefix(clean, MetaDir+"/") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	abs := filepath.Join(v.Root, filepath.FromSlash(clean))
	if !strings.HasPrefix(abs, v.Root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes vault", rel)
	}
	return abs, nil
}

// Rel normalises a path to the forward-slash vault-relative form used everywhere.
func (v *Vault) Rel(abs string) (string, error) {
	r, err := filepath.Rel(v.Root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(r), nil
}

// TypeOf derives the node type from the top-level folder.
func TypeOf(rel string) string {
	top, _, _ := strings.Cut(rel, "/")
	for t, d := range TypeDirs {
		if d == top {
			return t
		}
	}
	return "file"
}

// IsText reports whether a file's body is indexable text.
func IsText(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".md", ".markdown", ".txt", ".html", ".htm", ".svg", ".json", ".csv", ".tsv",
		".yaml", ".yml", ".xml", ".js", ".ts", ".jsx", ".tsx", ".css", ".py", ".go", ".sql", ".log":
		return true
	}
	return false
}

func isMD(rel string) bool {
	e := strings.ToLower(path.Ext(rel))
	return e == ".md" || e == ".markdown"
}

// SidecarPath is where the card for a non-MD file lives.
func (v *Vault) SidecarPath(rel string) string {
	return filepath.Join(v.Root, MetaDir, "meta", filepath.FromSlash(rel)+".json")
}

// Walk calls fn for every content file (meta dir skipped), with vault-relative paths.
func (v *Vault) Walk(fn func(rel string, info fs.FileInfo) error) error {
	return filepath.WalkDir(v.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == MetaDir || (p != v.Root && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := v.Rel(p)
		if err != nil {
			return err
		}
		return fn(rel, info)
	})
}

// maxBody caps how much text is read for indexing and summaries.
const maxBody = 256 << 10

// Read returns the card and (for text files) the body without frontmatter.
// The body is capped at maxBody bytes.
func (v *Vault) Read(rel string) (Card, string, error) {
	abs, err := v.Resolve(rel)
	if err != nil {
		return Card{}, "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Card{}, "", err
	}
	c := Card{}
	body := ""
	if IsText(rel) {
		f, err := os.Open(abs)
		if err != nil {
			return Card{}, "", err
		}
		raw, err := io.ReadAll(io.LimitReader(f, maxBody))
		f.Close()
		if err != nil {
			return Card{}, "", err
		}
		body = string(raw)
	}
	if isMD(rel) {
		fm, rest := splitFrontmatter(body)
		if fm != "" {
			_ = yaml.Unmarshal([]byte(fm), &c) // bad frontmatter → treat as no card, keep the file usable
		}
		body = strings.TrimLeft(rest, "\r\n")
	} else if raw, err := os.ReadFile(v.SidecarPath(rel)); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	c.Path, c.Type, c.Updated = rel, TypeOf(rel), info.ModTime().UTC()
	if c.Title == "" {
		c.Title = defaultTitle(rel, body)
	}
	if c.Summary == "" {
		c.Summary = fallbackSummary(rel, body)
	}
	c.Links = dedupe(append(c.Links, ExtractLinks(rel, body)...))
	return c, body, nil
}

// Write saves content and its card. For MD the card goes into frontmatter,
// otherwise into a sidecar JSON under .orbis/meta.
func (v *Vault) Write(c Card, content []byte) (Card, error) {
	abs, err := v.Resolve(c.Path)
	if err != nil {
		return Card{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Card{}, err
	}
	if isMD(c.Path) {
		_, body := splitFrontmatter(string(content)) // caller's frontmatter is replaced by the card
		fm, err := yaml.Marshal(c)
		if err != nil {
			return Card{}, err
		}
		content = []byte("---\n" + string(fm) + "---\n\n" + strings.TrimLeft(body, "\n"))
	} else if err := v.writeSidecar(c); err != nil {
		return Card{}, err
	}
	if err := writeAtomic(abs, content); err != nil {
		return Card{}, err
	}
	out, _, err := v.Read(c.Path)
	return out, err
}

// WriteRaw saves bytes exactly as given (the human editor path); the card is
// whatever the frontmatter or existing sidecar says.
func (v *Vault) WriteRaw(rel string, content []byte) error {
	abs, err := v.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return writeAtomic(abs, content)
}

// SaveCard updates only the card, leaving the file body untouched.
// ponytail: MD frontmatter keys Orbis doesn't know are dropped on rewrite; keep a yaml.Node if users complain.
func (v *Vault) SaveCard(c Card) error {
	if !isMD(c.Path) {
		if _, err := v.Resolve(c.Path); err != nil {
			return err
		}
		return v.writeSidecar(c)
	}
	abs, err := v.Resolve(c.Path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	_, body := splitFrontmatter(string(raw))
	_, err = v.Write(c, []byte(body))
	return err
}

// Import copies an outside file into the vault under the folder for its type.
func (v *Vault) Import(src string, c Card) (Card, error) {
	in, err := os.ReadFile(src)
	if err != nil {
		return Card{}, err
	}
	if c.Path == "" {
		t := TypeForExt(filepath.Ext(src))
		c.Path = v.FreePath(TypeDirs[t] + "/" + filepath.Base(src))
	}
	if c.Title == "" {
		c.Title = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	return v.Write(c, in)
}

// Delete removes a file and its sidecar.
func (v *Vault) Delete(rel string) error {
	abs, err := v.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil {
		return err
	}
	if err := os.Remove(v.SidecarPath(rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// TypeForExt picks the folder type for a file extension.
func TypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".md", ".markdown":
		return "note"
	case ".html", ".htm":
		return "page"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif":
		return "image"
	}
	return "file"
}

// DefaultExt is the extension used when an agent writes a type without a path.
func DefaultExt(typ string) string {
	switch typ {
	case "note":
		return ".md"
	case "page", "artifact":
		return ".html"
	}
	return ".txt"
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into a filename stem.
func Slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 80 {
		s = strings.TrimRight(s[:80], "-")
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

// FreePath returns rel, or rel with -2, -3 … appended if it already exists.
func (v *Vault) FreePath(rel string) string {
	ext := path.Ext(rel)
	stem := strings.TrimSuffix(rel, ext)
	for i := 1; ; i++ {
		cand := rel
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		abs, err := v.Resolve(cand)
		if err != nil {
			return cand // Resolve errors surface on write
		}
		if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
}

func (v *Vault) writeSidecar(c Card) error {
	p := v.SidecarPath(c.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	c.Path, c.Type, c.Updated = "", "", time.Time{}
	raw, err := json.MarshalIndent(struct {
		Title     string   `json:"title,omitempty"`
		Summary   string   `json:"summary,omitempty"`
		Tags      []string `json:"tags,omitempty"`
		Project   string   `json:"project,omitempty"`
		Links     []string `json:"links,omitempty"`
		CreatedBy string   `json:"created_by,omitempty"`
		Session   string   `json:"session,omitempty"`
	}{c.Title, c.Summary, c.Tags, c.Project, c.Links, c.CreatedBy, c.Session}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(p, raw)
}

// writeAtomic writes via a temp file + rename so a crash never leaves half a file.
func writeAtomic(abs string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".orbis-tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func splitFrontmatter(s string) (fm, body string) {
	s = strings.TrimPrefix(s, "\uFEFF")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", s
	}
	rest := s[strings.Index(s, "\n")+1:]
	for i := 0; i < len(rest); {
		j := strings.IndexByte(rest[i:], '\n')
		line := rest[i:]
		if j >= 0 {
			line = rest[i : i+j]
		}
		if strings.TrimRight(line, "\r") == "---" {
			if j < 0 {
				return rest[:i], ""
			}
			return rest[:i], rest[i+j+1:]
		}
		if j < 0 {
			break
		}
		i += j + 1
	}
	return "", s // unterminated: not frontmatter
}

var (
	htmlTag   = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	htmlTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	mdHeading = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	spaces    = regexp.MustCompile(`\s+`)
)

// PlainText strips markup for indexing and summaries.
func PlainText(rel, body string) string {
	e := strings.ToLower(path.Ext(rel))
	if e == ".html" || e == ".htm" || e == ".svg" {
		body = htmlTag.ReplaceAllString(body, " ")
	}
	return strings.TrimSpace(spaces.ReplaceAllString(body, " "))
}

func defaultTitle(rel, body string) string {
	if isMD(rel) {
		if m := mdHeading.FindStringSubmatch(body); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	if e := strings.ToLower(path.Ext(rel)); e == ".html" || e == ".htm" {
		if m := htmlTitle.FindStringSubmatch(body); m != nil {
			if t := strings.TrimSpace(m[1]); t != "" {
				return t
			}
		}
	}
	return strings.TrimSuffix(path.Base(rel), path.Ext(rel))
}

func fallbackSummary(rel, body string) string {
	t := PlainText(rel, body)
	if t == "" {
		return path.Base(rel)
	}
	r := []rune(t)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return t
}

var (
	wikiLink = regexp.MustCompile(`\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]`)
	mdLink   = regexp.MustCompile(`\]\(([^)\s]+)`)
	hrefLink = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']([^"']+)["']`)
)

// ExtractLinks returns link targets found in the body. Relative links are
// resolved against the file's folder; wikilinks are returned as "[[name]]"
// and resolved to a path by the index (which knows every filename).
func ExtractLinks(rel, body string) []string {
	var out []string
	for _, m := range wikiLink.FindAllStringSubmatch(body, -1) {
		out = append(out, "[["+strings.TrimSpace(m[1])+"]]")
	}
	var raws []string
	for _, m := range mdLink.FindAllStringSubmatch(body, -1) {
		raws = append(raws, m[1])
	}
	for _, m := range hrefLink.FindAllStringSubmatch(body, -1) {
		raws = append(raws, m[1])
	}
	for _, r := range raws {
		if strings.Contains(r, "://") || strings.HasPrefix(r, "#") || strings.HasPrefix(r, "mailto:") || strings.HasPrefix(r, "data:") {
			continue
		}
		r, _, _ = strings.Cut(r, "#")
		r, _, _ = strings.Cut(r, "?")
		if r == "" {
			continue
		}
		var p string
		if strings.HasPrefix(r, "/") {
			p = path.Clean(r)[1:]
		} else {
			p = path.Clean(path.Join(path.Dir(rel), r))
		}
		if p != "" && !strings.HasPrefix(p, "..") {
			out = append(out, p)
		}
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
