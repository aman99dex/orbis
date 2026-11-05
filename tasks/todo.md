# Orbis tasks

Order follows SPEC.md build order. Each task: tests green (`go test ./...`) before moving on.

## Slice 1 — agents can use it (no UI)
- [x] **vault**: layout, Resolve (path safety), card read/write (frontmatter + sidecar), import, slug paths, link extraction
  - Verify: `go test ./internal/vault`
- [x] **index**: SQLite schema, Sync (mtime diff), IndexFile/Remove, Search (FTS5, sanitized query, filters), List, Neighbors (hubs), Graph, Watch (fsnotify)
  - Verify: `go test ./internal/index`; 10k-file sync ≤ 10 s, search p95 ≤ 50 ms (bench test)
- [x] **mcp**: `orbis mcp` with search/read/write/import/neighbors/list
  - Verify: `go test ./internal/mcpserver` (in-memory client) + stdio smoke via built binary; register with Claude Code and write/search a real note

## Slice 2 — humans can use it
- [x] **ui-shell**: Wails app, tree, search, viewers (md/html/pdf/image/text), CodeMirror editor, live refresh from watcher
- [x] **graph**: sigma.js view, filters by type/hub kind, focus N hops
- [x] **slides**: MD → slides, HTML full-screen
- [x] **portability**: zip export/import (+ `orbis export|import` CLI)

## Slice 3 — extras + ship
- [x] **memory-bridge**: `orbis_recall` → Agentic_Memory REST when configured
- [~] **release** (repo aman99dex/orbis; first CI run caught a Linux-only path bug, fixed): GitHub Actions windows + macOS builds; size/memory check against success criteria

## Log
- 2026-11-04 Slice 1 done: `go test ./...` green (incl. stdio smoke). 10k sync 2.9 s, search p95 5 ms, 10 cards ≈ 960 tokens. Dogfooded with real `claude -p` + temp `--mcp-config`: write note + page, search, neighbors all correct. Binary 12.9 MB (no UI yet).
- 2026-11-04 Slices 2–3 built. UI checked by screenshots via `wails dev` + headless Edge CDP (reader, editor light/dark, HTML sandbox, PDF, orbit rings, overview, slides, search). Windowed `orbis.exe` serves MCP over stdio (risk retired, no second binary). Portable round trip byte-identical; zip-slip rejected. Memory bridge tested against a fake server.
- Measured on Windows, 1,000 notes: binary 19.9 MB ✅ (≤25). Warm start 0.7–1.7 s (target 1.5, noisy). First index of 1k files 2.4 s to window. Idle memory 259–285 MB total private ❌ (target 250): Orbis 57 MB, WebView2 ~200 MB of which GPU process ~95 MB. With `WebviewGpuIsDisabled` total drops to 187 MB but the WebGL graph falls back to software rendering — undecided.

## Open
- [x] GPU stays on (Aman 2026-11-06: works well > small). SPEC decision 7.
- [ ] macOS build never run (needs a Mac or CI). Release workflow never run.
- [ ] Live check: Claude writes via MCP while app open → shows within 1 s (watcher test proves <3 s at the index level; UI event wiring not observed end to end in the packaged app).
- [ ] "Block external loads in HTML pages" setting (SPEC decision 3) not built yet; external loads are allowed.
- 2026-11-06 Pushed to github.com/aman99dex/orbis (private). GPU stays on (decision 7). First CI run: tests failed on Linux — `..\x` accepted because `\` isn't a separator there; Resolve now treats `\` as a separator everywhere; CI tests now run on Linux + Windows. Agentic Memory local mode: Orbis calls the `agentic-memory` CLI; verified with the real AM code on a throwaway AGENTIC_MEMORY_HOME (2 turns → 3 facts via Haiku), real `claude -p` → `orbis_recall` returned user + project facts; app Memory view shows briefing + recall (screenshots).
- 2026-11-06 Aman: "if I am reading a page I can't scroll". Root cause: the app grid had no row height, so the row grew to the content and the reader/card panel never overflowed (screenshots looked fine; my one scripted scroll silently did nothing). Fixed with `grid-template-rows: minmax(0, 1fr)` + `min-height: 0`; confirmed by running the same real-wheel check on the old layout (fails) and the new one (passes). Slides now split at headings when a note has no `---`. `scripts/ui-check.mjs`: 14 checks with real mouse/keyboard input; all pass.
