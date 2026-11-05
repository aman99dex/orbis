<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../wailsjs/go/main/App'
  import { EventsOn } from '../wailsjs/runtime/runtime'
  import type { vault, main } from '../wailsjs/go/models'
  import Sidebar from './lib/Sidebar.svelte'
  import Viewer from './lib/Viewer.svelte'
  import Editor from './lib/Editor.svelte'
  import CardPanel from './lib/CardPanel.svelte'
  import Graph from './lib/Graph.svelte'
  import Slides from './lib/Slides.svelte'

  type Mode = 'view' | 'edit' | 'orbit'
  let cards = $state<vault.Card[]>([])
  let current = $state<string | null>(null)
  let doc = $state<main.Doc | null>(null)
  let mode = $state<Mode>('view')
  let presenting = $state(false)
  let error = $state('')
  let notice = $state('')
  let agentCmd = $state('')
  let theme = $state(readTheme())
  let searchEl = $state<HTMLInputElement>()

  function readTheme(): string {
    try { return localStorage.getItem('orbis-theme') || '' } catch { return '' }
  }
  $effect(() => {
    if (theme) document.documentElement.dataset.theme = theme
    else delete document.documentElement.dataset.theme
    try { localStorage.setItem('orbis-theme', theme) } catch {}
  })

  async function refresh() { cards = (await api.Cards()) ?? [] }

  async function open(path: string, m: Mode = mode === 'orbit' ? 'view' : mode) {
    try {
      doc = await api.Open(path)
      current = path
      mode = m === 'edit' && !doc.text ? 'view' : m
      error = ''
    } catch (e) { error = String(e) }
  }

  async function create(type: string) {
    try {
      const p = await api.Create(type, '')
      await refresh()
      await open(p, 'edit')
    } catch (e) { error = String(e) }
  }

  async function importFiles() {
    try {
      const ps = await api.Import()
      await refresh()
      if (ps?.length) open(ps[0], 'view')
    } catch (e) { error = String(e) }
  }

  async function remove(path: string) {
    if (await api.Delete(path)) {
      if (current === path) { current = null; doc = null }
      await refresh()
    }
  }

  async function exportZip() {
    try { const p = await api.ExportZip(); if (p) notice = `Exported to ${p}` } catch (e) { error = String(e) }
  }

  async function importZip() {
    try {
      const r = await api.ImportZip()
      if (!r) return
      await refresh()
      notice = `Imported ${r.added} files` + (r.unchanged ? `, ${r.unchanged} already here` : '') +
        (r.renamed?.length ? `. Kept both versions of ${r.renamed.length}: ${r.renamed.join(', ')}` : '')
    } catch (e) { error = String(e) }
  }

  function openWiki(name: string) {
    const n = name.toLowerCase()
    const hit = cards.find((c) => c.path.toLowerCase() === n) ??
      cards.find((c) => c.path.split('/').pop()!.replace(/\.[^.]+$/, '').toLowerCase() === n) ??
      cards.find((c) => c.title.toLowerCase() === n)
    if (hit) open(hit.path, 'view')
    else error = `No file named “${name}” yet.`
  }

  onMount(() => {
    refresh()
    api.AgentCommand().then((c) => (agentCmd = c))
    const off = EventsOn('vault:changed', async (paths: string[]) => {
      await refresh()
      if (current && paths.includes(current)) {
        if (!cards.some((c) => c.path === current)) { current = null; doc = null }
        else if (mode !== 'edit') doc = await api.Open(current) // never clobber an open editor
      }
    })
    const keys = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey
      if (mod && e.key === 'k') { e.preventDefault(); searchEl?.focus(); searchEl?.select() }
      else if (mod && e.key === 'n') { e.preventDefault(); create('note') }
      else if (mod && e.key === 'e' && doc?.text) { e.preventDefault(); mode = mode === 'edit' ? 'view' : 'edit' }
      else if (mod && e.key === 'g') { e.preventDefault(); mode = mode === 'orbit' ? 'view' : 'orbit' }
      else if (e.key === 'F5' && current) { e.preventDefault(); presenting = true }
    }
    window.addEventListener('keydown', keys)
    return () => { off(); window.removeEventListener('keydown', keys) }
  })

  const canPresent = $derived(!!doc && (doc.card.type === 'note' || /\.html?$/i.test(doc.card.path)))
</script>

<div class="shell">
  <Sidebar {cards} {current} bind:searchEl onopen={(p) => open(p)} oncreate={create} onimport={importFiles} onexport={exportZip} onimportzip={importZip} />

  <main>
    <header>
      {#if doc && mode !== 'orbit'}
        <span class="crumb" title={doc.card.path}>{doc.card.path}</span>
      {:else}
        <span class="crumb muted">{mode === 'orbit' ? 'Orbit' : 'Orbis'}</span>
      {/if}
      <nav aria-label="Mode">
        <button class="quiet" aria-pressed={mode === 'view'} disabled={!doc} onclick={() => (mode = 'view')}>Read</button>
        <button class="quiet" aria-pressed={mode === 'edit'} disabled={!doc?.text} onclick={() => (mode = 'edit')} title="Ctrl+E">Edit</button>
        <button class="quiet" disabled={!canPresent} onclick={() => (presenting = true)} title="F5">Present</button>
        <button class="quiet" aria-pressed={mode === 'orbit'} onclick={() => (mode = mode === 'orbit' ? 'view' : 'orbit')} title="Ctrl+G">Orbit</button>
        <button class="quiet" title="Switch theme" aria-label="Switch theme"
          onclick={() => (theme = theme === '' ? 'dark' : theme === 'dark' ? 'light' : '')}>{theme === '' ? 'Auto' : theme === 'dark' ? 'Dark' : 'Light'}</button>
      </nav>
    </header>
    {#if error}<p class="error" role="alert">{error} <button class="quiet" onclick={() => (error = '')}>Dismiss</button></p>{/if}
    {#if notice}<p class="notice" role="status">{notice} <button class="quiet" onclick={() => (notice = '')}>Dismiss</button></p>{/if}

    <section class="stage">
      {#if mode === 'orbit'}
        <Graph focus={current} onopen={(p) => open(p, 'view')} />
      {:else if doc && mode === 'edit'}
        {#key doc.card.path}
          <Editor {doc} onsaved={refresh} onerror={(e) => (error = e)} />
        {/key}
      {:else if doc}
        <Viewer {doc} onopen={(p) => open(p, 'view')} onwiki={openWiki} onexternal={(u) => api.OpenExternal(u)} />
      {:else}
        <div class="empty">
          {#if cards.length === 0}
            <h1>Nothing here yet</h1>
            <p>Write a note, add files, or let an agent fill it. To connect Claude Code, run this once in a terminal:</p>
          {:else}
            <h1>{cards.length} {cards.length === 1 ? 'file' : 'files'} in your Orbis</h1>
            <p>Pick one on the left, search with <kbd>Ctrl K</kbd>, or open the orbit with <kbd>Ctrl G</kbd>. To connect Claude Code:</p>
          {/if}
          <pre>{agentCmd}</pre>
          <div class="row">
            <button onclick={() => create('note')}>New note</button>
            <button onclick={importFiles}>Add files</button>
            <button onclick={() => navigator.clipboard.writeText(agentCmd)}>Copy command</button>
          </div>
        </div>
      {/if}
    </section>
  </main>

  {#if doc && mode !== 'orbit'}
    <CardPanel {doc} onopen={(p) => open(p, 'view')} onsaved={async () => { await refresh(); if (current) doc = await api.Open(current) }}
      ondelete={remove} onerror={(e) => (error = e)} />
  {/if}
</div>

{#if presenting && doc}
  <Slides {doc} onclose={() => (presenting = false)} />
{/if}

<style>
  .shell { display: grid; grid-template-columns: 264px minmax(0, 1fr) auto; height: 100vh; }
  main { display: flex; flex-direction: column; min-width: 0; border-left: 1px solid var(--rule); }
  header { display: flex; align-items: center; gap: var(--s3); padding: var(--s2) var(--s3); border-bottom: 1px solid var(--rule); min-height: 41px; }
  .crumb { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 12px var(--mono); color: var(--ink-2); }
  nav { display: flex; gap: 2px; }
  .stage { flex: 1; min-height: 0; overflow: auto; position: relative; }
  .notice { margin: 0; padding: var(--s2) var(--s3); color: var(--ink-2); border-bottom: 1px solid var(--rule); background: var(--brass-soft); }
  .error { margin: 0; padding: var(--s2) var(--s3); color: var(--danger); border-bottom: 1px solid var(--rule); }
  .empty { max-width: 60ch; margin: 12vh auto 0; padding: 0 var(--s5); }
  .empty h1 { font: 600 26px/1.2 var(--read); margin: 0 0 var(--s3); }
  .empty p { color: var(--ink-2); }
  .empty pre { font: 12px/1.5 var(--mono); background: var(--paper-2); padding: var(--s3); border-radius: 8px; white-space: pre-wrap; word-break: break-all; }
  .row { display: flex; gap: var(--s2); }
  @media (max-width: 900px) { .shell { grid-template-columns: 220px minmax(0, 1fr); } }
</style>
