// Package index is a disposable SQLite cache over the vault: cards, full-text
// search, and the edge list behind neighbors/graph. Delete index.db and Sync
// rebuilds it.
package index

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"orbis/internal/vault"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS nodes(
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  stem TEXT NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  tags TEXT NOT NULL,
  project TEXT NOT NULL,
  created_by TEXT NOT NULL,
  session TEXT NOT NULL,
  updated INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  size INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS nodes_stem ON nodes(stem);
CREATE INDEX IF NOT EXISTS nodes_updated ON nodes(updated);
CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(title, summary, tags, body, tokenize='porter unicode61');
-- kind: link (dst = path, or "[[name]]" until resolved) | tag | project | session (dst = hub key)
CREATE TABLE IF NOT EXISTS edges(src TEXT NOT NULL, kind TEXT NOT NULL, dst TEXT NOT NULL, PRIMARY KEY(src, kind, dst));
CREATE INDEX IF NOT EXISTS edges_dst ON edges(kind, dst);
`

type Index struct {
	db *sql.DB
	V  *vault.Vault
}

// Open opens (or creates) the cache at <vault>/.orbis/index.db.
func Open(v *vault.Vault) (*Index, error) {
	p := filepath.Join(v.Root, vault.MetaDir, "index.db")
	db, err := sql.Open("sqlite", p+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // ponytail: one conn per process; WAL + busy_timeout handle the GUI/MCP pair
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init index: %w", err)
	}
	return &Index{db: db, V: v}, nil
}

func (ix *Index) Close() error { return ix.db.Close() }

// Sync brings the cache in line with the folder: indexes new/changed files
// (by mtime+size) and drops deleted ones. Returns how many files were (re)indexed.
func (ix *Index) Sync() (int, error) {
	known := map[string][2]int64{}
	rows, err := ix.db.Query(`SELECT path, mtime, size FROM nodes`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var p string
		var m, s int64
		if err := rows.Scan(&p, &m, &s); err != nil {
			rows.Close()
			return 0, err
		}
		known[p] = [2]int64{m, s}
	}
	rows.Close()

	tx, err := ix.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	err = ix.V.Walk(func(rel string, info fs.FileInfo) error {
		k, ok := known[rel]
		delete(known, rel)
		m := ix.mtime(rel, info)
		if ok && k == [2]int64{m, info.Size()} {
			return nil
		}
		n++
		return ix.put(tx, rel, m, info.Size(), ok)
	})
	if err != nil {
		return n, err
	}
	for gone := range known {
		if err := del(tx, gone); err != nil {
			return n, err
		}
	}
	if err := resolveWikilinks(tx); err != nil {
		return n, err
	}
	return n, tx.Commit()
}

// mtime is the newer of the file and its sidecar card, so editing only the
// card of a PDF still counts as a change.
func (ix *Index) mtime(rel string, info fs.FileInfo) int64 {
	m := info.ModTime().UnixNano()
	if e := strings.ToLower(path.Ext(rel)); e == ".md" || e == ".markdown" {
		return m // MD cards live in frontmatter, never a sidecar
	}
	if si, err := statSidecar(ix.V, rel); err == nil && si.ModTime().UnixNano() > m {
		m = si.ModTime().UnixNano()
	}
	return m
}

// Put (re)indexes one file now; used after writes so search sees them immediately.
func (ix *Index) Put(rel string) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	abs, err := ix.V.Resolve(rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if err := ix.put(tx, rel, ix.mtime(rel, info), info.Size(), true); err != nil {
		return err
	}
	if err := resolveWikilinks(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Remove drops one file from the cache.
func (ix *Index) Remove(rel string) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := del(tx, rel); err != nil {
		return err
	}
	return tx.Commit()
}

// put indexes one file. mtime/size are what Sync compares next time;
// existed=false skips the delete for files the cache has never seen.
func (ix *Index) put(tx *sql.Tx, rel string, mtime, size int64, existed bool) error {
	c, body, err := ix.V.Read(rel)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	if existed {
		if err := del(tx, rel); err != nil {
			return err
		}
	}
	tags, _ := json.Marshal(nonNil(c.Tags))
	stem := strings.ToLower(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	res, err := tx.Exec(`INSERT INTO nodes(path,stem,type,title,summary,tags,project,created_by,session,updated,mtime,size)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		rel, stem, c.Type, c.Title, c.Summary, string(tags), c.Project, c.CreatedBy, c.Session,
		c.Updated.Unix(), mtime, size)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO fts(rowid,title,summary,tags,body) VALUES(?,?,?,?,?)`,
		id, c.Title, c.Summary, strings.Join(c.Tags, " ")+" "+c.Project, vault.PlainText(rel, body)); err != nil {
		return err
	}
	edge := func(kind, dst string) error {
		if dst == "" || (kind == "link" && dst == rel) {
			return nil
		}
		_, err := tx.Exec(`INSERT OR IGNORE INTO edges(src,kind,dst) VALUES(?,?,?)`, rel, kind, dst)
		return err
	}
	for _, l := range c.Links {
		if err := edge("link", l); err != nil {
			return err
		}
	}
	for _, t := range c.Tags {
		if err := edge("tag", strings.ToLower(strings.TrimPrefix(t, "#"))); err != nil {
			return err
		}
	}
	if err := edge("project", strings.ToLower(c.Project)); err != nil {
		return err
	}
	return edge("session", c.Session)
}

func del(tx *sql.Tx, rel string) error {
	if _, err := tx.Exec(`DELETE FROM fts WHERE rowid=(SELECT id FROM nodes WHERE path=?)`, rel); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE path=?`, rel); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM edges WHERE src=?`, rel)
	return err
}

// resolveWikilinks turns "[[name]]" link targets into paths once a file with
// that stem (or exact path) exists. Unresolvable ones stay raw and are ignored.
func resolveWikilinks(tx *sql.Tx) error {
	_, err := tx.Exec(`
UPDATE OR IGNORE edges SET dst = (
  SELECT n.path FROM nodes n
  WHERE n.stem = lower(substr(edges.dst, 3, length(edges.dst) - 4))
     OR n.path = substr(edges.dst, 3, length(edges.dst) - 4)
  ORDER BY (n.type = 'note') DESC, length(n.path) LIMIT 1)
WHERE kind = 'link' AND dst LIKE '[[%]]' AND EXISTS (
  SELECT 1 FROM nodes n
  WHERE n.stem = lower(substr(edges.dst, 3, length(edges.dst) - 4))
     OR n.path = substr(edges.dst, 3, length(edges.dst) - 4))`)
	return err
}

// Query filters for Search/List. Zero values mean "any".
type Query struct {
	Text    string
	Type    string
	Tag     string
	Project string
	Limit   int
}

const cardCols = `n.path, n.type, n.title, n.summary, n.tags, n.project, n.created_by, n.session, n.updated`

func (q Query) filters() (string, []any) {
	var where []string
	var args []any
	if q.Type != "" {
		where = append(where, "n.type = ?")
		args = append(args, q.Type)
	}
	if q.Project != "" {
		where = append(where, "lower(n.project) = lower(?)")
		args = append(args, q.Project)
	}
	if q.Tag != "" {
		where = append(where, "EXISTS(SELECT 1 FROM edges e WHERE e.src = n.path AND e.kind = 'tag' AND e.dst = lower(?))")
		args = append(args, strings.TrimPrefix(q.Tag, "#"))
	}
	if len(where) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(where, " AND "), args
}

func (q Query) limit() int {
	if q.Limit <= 0 || q.Limit > 100 {
		return 10
	}
	return q.Limit
}

// Search ranks cards by full-text relevance (title > summary > tags > body).
// Empty text falls back to List (most recent first).
func (ix *Index) Search(q Query) ([]vault.Card, error) {
	toks := tokens(q.Text)
	if len(toks) == 0 {
		return ix.List(q)
	}
	where, args := q.filters()
	run := func(match string) ([]vault.Card, error) {
		return ix.cards(`SELECT `+cardCols+` FROM fts JOIN nodes n ON n.id = fts.rowid
			WHERE fts MATCH ?`+where+` ORDER BY bm25(fts, 10.0, 5.0, 3.0, 1.0) LIMIT ?`,
			append(append([]any{match}, args...), q.limit())...)
	}
	out, err := run(strings.Join(toks, " "))
	if err != nil || len(out) > 0 || len(toks) == 1 {
		return out, err
	}
	return run(strings.Join(toks, " OR ")) // nothing matched every word: rank by any
}

// List returns cards, most recently updated first.
func (ix *Index) List(q Query) ([]vault.Card, error) {
	where, args := q.filters()
	return ix.cards(`SELECT `+cardCols+` FROM nodes n WHERE 1=1`+where+` ORDER BY n.updated DESC, n.path LIMIT ?`,
		append(args, q.limit())...)
}

// All returns every card, newest first (the sidebar).
func (ix *Index) All() ([]vault.Card, error) {
	return ix.cards(`SELECT ` + cardCols + ` FROM nodes n ORDER BY n.updated DESC, n.path`)
}

// GNode is a file or a hub (tag/project/session) in the graph view.
type GNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // note|page|artifact|pdf|image|file|tag|project|session
	Agent bool   `json:"agent,omitempty"`
}

// Graph returns every file, every hub and every edge whose ends exist.
func (ix *Index) Graph() ([]GNode, []Edge, error) {
	var nodes []GNode
	rows, err := ix.db.Query(`SELECT path, title, type, created_by FROM nodes`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var n GNode
		var by string
		if err := rows.Scan(&n.ID, &n.Label, &n.Kind, &by); err != nil {
			rows.Close()
			return nil, nil, err
		}
		n.Agent = by != "" && by != "human"
		nodes = append(nodes, n)
	}
	rows.Close()

	var edges []Edge
	hubs := map[string]bool{}
	rows, err = ix.db.Query(`
SELECT e.src, CASE e.kind WHEN 'link' THEN e.dst ELSE e.kind || ':' || e.dst END, e.kind FROM edges e
WHERE e.kind != 'link' OR e.dst IN (SELECT path FROM nodes)`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind); err != nil {
			return nil, nil, err
		}
		if e.Kind != "link" && !hubs[e.To] {
			hubs[e.To] = true
			_, label, _ := strings.Cut(e.To, ":")
			nodes = append(nodes, GNode{ID: e.To, Label: label, Kind: e.Kind})
		}
		edges = append(edges, e)
	}
	return nodes, edges, rows.Err()
}

// Card returns one card from the cache.
func (ix *Index) Card(rel string) (vault.Card, error) {
	cs, err := ix.cards(`SELECT `+cardCols+` FROM nodes n WHERE n.path = ?`, rel)
	if err != nil {
		return vault.Card{}, err
	}
	if len(cs) == 0 {
		return vault.Card{}, fmt.Errorf("%s: %w", rel, fs.ErrNotExist)
	}
	return cs[0], nil
}

func (ix *Index) cards(query string, args ...any) ([]vault.Card, error) {
	rows, err := ix.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []vault.Card
	for rows.Next() {
		var c vault.Card
		var tags string
		var updated int64
		if err := rows.Scan(&c.Path, &c.Type, &c.Title, &c.Summary, &tags, &c.Project, &c.CreatedBy, &c.Session, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tags), &c.Tags)
		c.Updated = time.Unix(updated, 0).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

var word = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// tokens turns free text into safe FTS5 terms: each word quoted, last one a prefix.
func tokens(s string) []string {
	ws := word.FindAllString(s, 12)
	for i, w := range ws {
		ws[i] = `"` + w + `"`
	}
	if n := len(ws); n > 0 {
		ws[n-1] += "*"
	}
	return ws
}

// Edge connects a file to another file (link) or to a hub (tag/project/session).
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// maxHub skips hubs bigger than this when walking neighbors, so a tag on
// hundreds of files doesn't make everything a neighbor of everything.
const maxHub = 50

// Neighbors walks up to depth hops from rel. Links count as one hop in either
// direction; file → shared tag/project/session → file also counts as one hop.
// The start card comes first.
func (ix *Index) Neighbors(rel string, depth int) ([]vault.Card, []Edge, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	start, err := ix.Card(rel)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{rel: true}
	order := []string{rel}
	frontier := []string{rel}
	var edges []Edge
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, f := range frontier {
			es, err := ix.adjacent(f)
			if err != nil {
				return nil, nil, err
			}
			for _, e := range es {
				edges = append(edges, e)
				other := e.To
				if e.Kind == "link" && e.To == f {
					other = e.From
				} else if e.Kind != "link" {
					other = e.From // hub edges are reported as member → hub key; From is the member
				}
				if !seen[other] {
					seen[other] = true
					order = append(order, other)
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	cards := []vault.Card{start}
	for _, p := range order[1:] {
		if c, err := ix.Card(p); err == nil {
			cards = append(cards, c)
		}
	}
	return cards, dedupeEdges(edges), nil
}

func (ix *Index) adjacent(f string) ([]Edge, error) {
	var out []Edge
	rows, err := ix.db.Query(`
SELECT e.src, e.dst, 'link' FROM edges e JOIN nodes n ON n.path = e.dst WHERE e.kind = 'link' AND e.src = ?1
UNION
SELECT e.src, e.dst, 'link' FROM edges e WHERE e.kind = 'link' AND e.dst = ?1
UNION
SELECT src, kind || ':' || dst, kind FROM edges WHERE src = ?1 AND kind IN ('tag','project','session')
UNION
SELECT m.src, m.kind || ':' || m.dst, m.kind FROM edges h
  JOIN edges m ON m.kind = h.kind AND m.dst = h.dst AND m.src != h.src
  WHERE h.src = ?1 AND h.kind IN ('tag','project','session')
    AND (SELECT count(*) FROM edges c WHERE c.kind = h.kind AND c.dst = h.dst) <= ?2`, f, maxHub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func dedupeEdges(es []Edge) []Edge {
	seen := map[Edge]bool{}
	out := es[:0]
	for _, e := range es {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func statSidecar(v *vault.Vault, rel string) (fs.FileInfo, error) { return os.Stat(v.SidecarPath(rel)) }
