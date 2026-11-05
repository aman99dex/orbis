# Spec: Orbis

Status: **approved 2026-11-04** (defaults taken for open questions — see bottom). Intent: [docs/intent/orbis.md](docs/intent/orbis.md).

## Objective

A local-only desktop app that keeps context (MD, HTML, artifacts, PDFs, images) in one plain folder,
lets humans view/edit/present it, and lets any coding agent read and write it over MCP — with a cheap
"node card" layer so agents fetch summaries before full files.

User stories:
- As a human, I open Orbis and browse/search/edit everything in `~/Orbis/`, see it as a graph, present an MD file as slides.
- As an agent (Claude Code), after `claude mcp add orbis -- orbis mcp`, I can save a note with a summary and later find it by search in ~1–2k tokens instead of reading whole files.
- As a human moving PCs, I export a zip, import it on the other machine, and everything (cards, links, graph) is back.

## Capability map

| Module id | Responsibility | Depends on |
|---|---|---|
| `vault` | Folder layout, file CRUD, frontmatter + sidecar cards, path safety, file watcher | — |
| `index` | SQLite cache: cards, FTS5 search, links, tags; rebuildable from `vault` | vault |
| `mcp` | `orbis mcp` stdio server exposing search/read/write/neighbors/list/import | index, vault |
| `ui-shell` | Wails window: sidebar tree, search, viewers (md/html/pdf/image/text), MD/HTML editor | index, vault |
| `graph` | Graph view, filters, focus N hops | index, ui-shell |
| `slides` | Present MD as slides, HTML full-screen | ui-shell |
| `portability` | Zip export/import, reindex after import | vault, index |
| `memory-bridge` | Optional forward to Agentic_Memory (`orbis_recall`) when configured | mcp |
| `release` | CI builds: Windows `.exe`, macOS universal `.app`/`.dmg` | all |

Build order: `vault → index → mcp` (dogfood with Claude Code from here) `→ ui-shell → graph, slides, portability → memory-bridge → release`.

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| App shell | **Wails v2** (Go + system webview: WebView2 / WKWebView) | Small binary, low memory, Go already installed; Tauri would need Rust installed |
| Language | Go 1.27 | |
| Index | `modernc.org/sqlite` (pure Go, FTS5) | No CGO for the DB |
| MCP | `github.com/modelcontextprotocol/go-sdk` | Official SDK, stdio transport |
| Watcher | `github.com/fsnotify/fsnotify` | |
| Zip | Go stdlib `archive/zip` | |
| Frontend | Svelte 5 + TypeScript + Vite (Wails template) | Compiles small, no runtime framework weight |
| Editor | CodeMirror 6 | MD + HTML modes |
| MD render | markdown-it | |
| Graph | sigma.js + graphology (WebGL) | Stays smooth at 10k nodes |
| PDF / images | Native webview (`<iframe>` / `<img>`) | No PDF library needed |

## Storage layout

```
~/Orbis/                      (configurable root)
  notes/      *.md            cards in YAML frontmatter
  pages/      *.html          human/agent-authored pages
  artifacts/  *.html *.svg *.json *.csv ...   agent outputs
  pdfs/       *.pdf
  images/     *.png *.jpg *.svg *.webp
  files/      anything else
  .orbis/meta/<relpath>.json  cards for non-MD files (included in zip)
  .orbis/index.db             SQLite cache (excluded from zip, rebuilt on demand)
```

Subfolders inside each type folder are free-form. Grouping across types uses the `project:` field.

Node card (MD frontmatter, or sidecar JSON with the same keys):

```yaml
---
title: COL-131 gateway rollout
summary: Order of work for putting Collections behind the gateway; blocked on FE #9.
tags: [collections, gateway]
project: col-131
links: [pages/col-131-arch.html]
created_by: claude-code      # or "human"
session: 2026-11-04-a1        # files written in one agent session share it → graph edge
---
```

Missing summary → first ~200 chars of text (MD/HTML) or filename (binary).
Links are also parsed from `[[wikilinks]]`, MD links and HTML `<a href>` to vault paths.

## MCP tools

| Tool | Input | Returns |
|---|---|---|
| `orbis_search` | `query`, `type?`, `tag?`, `project?`, `limit=10` | cards (path, type, title, summary, tags, updated) |
| `orbis_read` | `path`, `max_chars?` | text content; for PDF/image: card + absolute path |
| `orbis_write` | `type`, `title`, `content`, `summary`, `tags?`, `project?`, `path?`, `session?` | path written |
| `orbis_import` | `src_path`, `summary`, `tags?`, `project?` | path (copies a PDF/image/any file in) |
| `orbis_neighbors` | `path`, `depth=1` | cards + edges |
| `orbis_list` | `type?`, `project?`, `limit=20` (most recent first) | cards |
| `orbis_recall` | `query` | only registered when memory-bridge is configured |

`orbis mcp` runs headless (no window), so agents work even when the app is closed. GUI and MCP processes share the folder; each watches it and updates the SQLite cache (WAL mode).

## Commands

```
Dev:     wails dev
Build:   wails build                      # current OS
Test:    go test ./...
Lint:    golangci-lint run
MCP:     orbis mcp                        # stdio server
Agent:   claude mcp add orbis -- <path-to>/orbis mcp
```

## Project structure

```
main.go              → entry: `orbis` (GUI) | `orbis mcp` (stdio)
app.go               → Wails-bound methods for the frontend
internal/vault/      → layout, CRUD, cards, path safety, watcher
internal/index/      → SQLite schema, indexing, search, graph queries
internal/mcpserver/  → MCP tool handlers
internal/portable/   → zip export/import
frontend/src/        → Svelte UI (viewers, editor, graph, slides)
docs/decisions/      → ADRs
docs/learn/          → concept notes (written after things work)
.github/workflows/   → release builds
```

## Code style

Go: stdlib-first, errors wrapped with context, no interfaces with one implementation.

```go
// Resolve returns an absolute path inside root, or an error if rel escapes it.
func (v *Vault) Resolve(rel string) (string, error) {
	abs := filepath.Join(v.Root, filepath.Clean("/"+rel))
	if !strings.HasPrefix(abs, v.Root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes vault", rel)
	}
	return abs, nil
}
```

## Testing strategy

- `go test ./...` on core logic: vault (path safety, cards), index (search, links, rebuild), portable (zip roundtrip), mcpserver (each tool against a temp vault).
- One stdio smoke test: spawn `orbis mcp`, call `orbis_write` then `orbis_search`.
- UI: manual checks via running the app; no frontend test framework in v1.
- Tests use `t.TempDir()` vaults — never the real `~/Orbis`.

## Boundaries

- **Always:** keep files the source of truth (index is disposable); validate every agent-supplied path stays in the vault; make zero network calls from the app shell; run `go test ./...` before commits.
- **Ask first:** new dependencies beyond the table above; code signing / notarization setup; changing the storage layout or card keys after v1.
- **Never:** send data anywhere off-machine; delete user files without explicit action; let HTML pages access the Wails bridge (sandboxed iframe, no same-origin); add Claude attribution to commits.

## Success criteria

1. Windows `.exe` and macOS `.app` each ≤ 25 MB; run with nothing else installed (WebView2 is built into Windows 11).
2. ~~Idle memory ≤ 250 MB~~ dropped (decision 7); Orbis's own process stays lean (measured 57 MB). Warm start to usable ≤ 1.5 s.
3. Initial index of 10,000 files ≤ 10 s; `orbis_search` p95 ≤ 50 ms.
4. `orbis_search` with 10 results ≤ 1,500 tokens.
5. Claude Code writes a note via MCP → it appears in the open app within 1 s.
6. Zip export on one machine → import on another → identical file hashes; search and graph work after reindex.
7. Every file type renders: MD, HTML, PDF, PNG/JPG/SVG/WebP, plain text/JSON/CSV.
8. MD file presents as slides (`---` separators, arrow keys, Esc to exit).
9. Graph shows all file types; focusing a node shows only nodes within N hops.

## Risks

- **Windows GUI binary + stdio:** a GUI-subsystem `.exe` may not get usable stdin/stdout when spawned by an agent. Verify in the `mcp` slice; fallback is a tiny second binary `orbis-mcp` shipped alongside.
- **macOS builds** need a Mac (or CI `macos-latest`); unsigned apps show a Gatekeeper warning until signing is set up.
- **Two processes, one SQLite file:** WAL + short transactions; if contention shows up, the MCP process becomes read-only and writes files only (watcher indexes them).

## Decisions (resolved 2026-11-04)

1. **Wails (Go)** over Tauri (Rust): Go installed, Rust not.
2. **Type-first layout** (`notes/ pages/ …`), `project:` field groups across types.
3. **External loads in HTML pages allowed** by default; a setting blocks them.
4. **Unsigned v1** builds; signing later.
5. **Graph hubs:** tags, projects and sessions are hub nodes (file → #tag), not pairwise edges, so a popular tag never creates N² edges. Neighbors treats file→hub→file as one hop and skips hubs with > 50 members.
6. Added `gopkg.in/yaml.v3` for frontmatter.
7. **GPU stays on** (2026-11-06, Aman: "I don't care for size, I want it to work well"). WebView2's GPU process costs ~95 MB (total ~260 MB idle at 1k files); turning it off reached 187 MB but forces software WebGL for the orbit graph.
8. **Personal repo:** `github.com/aman99dex/orbis` (not the neofloai org).
9. **Agentic Memory works locally** (2026-11-06, Aman): besides `MEMORY_URL`, Orbis runs the `agentic-memory` CLI (PATH or `ORBIS_MEMORY_CMD`) so no server is needed; agents pass their project folder, the app shows a Memory view (briefing + recall).
