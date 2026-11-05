# Intent: Orbis (confirmed 2026-11-04)

- **What:** one small standalone desktop binary (Mac + Windows), no Docker, no runtime, no network.
- **Storage:** `~/Orbis/` split by type (`notes/` md, `pages/` html, `artifacts/`, `pdfs/`, `images/`); every file stays plain and human-readable.
- **Humans:** native viewers for every type, MD/HTML editor, slides from MD (split on `---`), HTML full-screen; humans create anything agents can.
- **Agents:** built-in MCP server — Claude Code, Cursor, any MCP client creates, searches, reads context automatically.
- **Memory layer (v1, built in, no LLM):** every file has a node card (title, summary, tags, links). Agents pull cheap cards first, full files only when needed. The writing agent supplies the summary. Agentic_Memory pluggable as optional extra.
- **Graph:** every file type is a node; edges from links, tags, memory relations; focus view (N hops from a node).
- **Portability:** export folder → zip, import zip on another PC.
- **Users:** Aman first, then anyone who downloads it.
- **Out of scope v1:** cloud sync, accounts, collaboration, Notion-style databases, mobile, plugins, built-in AI chat, bundled local LLM.

Why not Obsidian + MCP plugin: Electron-heavy, notes-first (PDF/HTML second-class), agent wiring is plugin stitching.
