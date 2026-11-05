# Orbis

Local-first context for humans and coding agents. One small desktop app keeps your notes, HTML pages,
agent artifacts, PDFs and images in a plain folder, lets you read, edit and present them, shows how they
connect, and lets any MCP-capable agent (Claude Code, Cursor, …) search and write to the same place.
Nothing leaves your machine.

## Use it

```
orbis                       # open the app (folder: ~/Orbis, or -root DIR, or $ORBIS_ROOT)
orbis mcp                   # MCP server on stdio, for agents
orbis export orbis.zip      # whole Orbis as one zip
orbis import orbis.zip      # merge a zip in; never overwrites (conflicts kept as "name (imported).md")
```

Connect Claude Code once (the empty screen in the app shows this with your real paths):

```
claude mcp add orbis --scope user -- "C:\path\to\orbis.exe" mcp
```

## How it stores things

```
~/Orbis/
  notes/  pages/  artifacts/  pdfs/  images/  files/   ← your files, plain and readable
  .orbis/meta/   cards for non-Markdown files (travels in the zip)
  .orbis/index.db  search cache (rebuilt automatically; safe to delete)
```

Every file has a **card**: title, summary, tags, project, who wrote it, session. Markdown keeps it in
frontmatter; everything else in a sidecar JSON. Agents read cards first (10 results ≈ 1k tokens) and open
full files only when needed. The writing agent supplies the summary, so Orbis needs no LLM of its own.

## Agent tools

| Tool | What it does |
|---|---|
| `orbis_search` | Ranked full-text search → cards |
| `orbis_list` | Recent cards, filter by type / tag / project |
| `orbis_neighbors` | Linked files and files sharing a tag, project or session |
| `orbis_read` | One file's card + text (PDFs/images: card + absolute path) |
| `orbis_write` | Create or update a note/page/artifact with its card |
| `orbis_import` | Copy a PDF, image or any file in with a card |
| `orbis_recall` | Only when `MEMORY_URL` is set: recall from an [Agentic Memory](../Agentic_Memory) service (`MEMORY_API_KEY`, `MEMORY_USER`) |

## The app

Sidebar by type with search (`Ctrl K`), reader for Markdown/HTML/PDF/images/text, editor with autosave
(`Ctrl E`), card panel (what agents see), **Orbit** graph (`Ctrl G`) where the focused file sits in the
centre and each hop is a ring, slides from Markdown split on `---` (`F5`), export/import in the footer.
HTML pages run in a sandbox that can't reach the app.

## Develop

```
go test ./...                       # core + MCP + stdio smoke (builds the binary)
go test -short ./...                # skip the 10k-file scale test and the binary build
wails dev                           # app with hot reload
wails build                         # build/bin/orbis.exe (or Orbis.app on macOS)
ORBIS_BIN=build/bin/orbis.exe go test . -run TestStdioSmoke   # prove the GUI build serves MCP too
```

Spec and decisions: [SPEC.md](SPEC.md). Task log: [tasks/todo.md](tasks/todo.md).
Releases: push a `v*` tag; [.github/workflows/release.yml](.github/workflows/release.yml) builds Windows and macOS.
