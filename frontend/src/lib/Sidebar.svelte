<script lang="ts">
  import * as api from '../../wailsjs/go/main/App'
  import type { vault } from '../../wailsjs/go/models'

  let { cards, current, searchEl = $bindable(), onopen, oncreate, onimport, onexport, onimportzip }: {
    cards: vault.Card[]; current: string | null; searchEl?: HTMLInputElement
    onopen: (p: string) => void; oncreate: (t: string) => void; onimport: () => void; onexport: () => void; onimportzip: () => void
  } = $props()

  const groups: [string, string][] = [['note', 'Notes'], ['page', 'Pages'], ['artifact', 'Artifacts'], ['pdf', 'PDFs'], ['image', 'Images'], ['file', 'Other files']]
  let q = $state('')
  let results = $state<vault.Card[] | null>(null)
  let timer: ReturnType<typeof setTimeout>

  $effect(() => {
    const text = q.trim()
    clearTimeout(timer)
    if (!text) { results = null; return }
    timer = setTimeout(async () => { results = (await api.Search(text)) ?? [] }, 120)
  })

  const byType = $derived(Object.fromEntries(groups.map(([t]) => [t, cards.filter((c) => c.type === t)])))
  const isAgent = (c: vault.Card) => !!c.created_by && c.created_by !== 'human'

  function keyNav(e: KeyboardEvent) {
    if (e.key === 'Enter' && results?.length) onopen(results[0].path)
    if (e.key === 'Escape') q = ''
  }
</script>

<aside>
  <div class="search">
    <input bind:this={searchEl} bind:value={q} onkeydown={keyNav} placeholder="Search everything" aria-label="Search" spellcheck="false" />
  </div>
  <div class="actions">
    <button onclick={() => oncreate('note')} title="Ctrl+N">New note</button>
    <button onclick={() => oncreate('page')}>New page</button>
    <button onclick={onimport}>Add files</button>
  </div>

  <div class="list">
    {#if results}
      {#if results.length === 0}
        <p class="muted pad">No matches for “{q}”.</p>
      {/if}
      {#each results as c (c.path)}
        <button class="item hit" class:on={c.path === current} onclick={() => onopen(c.path)}>
          <span class="t"><span class="dot {isAgent(c) ? 'agent' : 'human'}"></span>{c.title}</span>
          <span class="s">{c.summary}</span>
        </button>
      {/each}
    {:else}
      {#each groups as [t, label] (t)}
        {#if byType[t].length}
          <details open>
            <summary><span class="kind" style="background: var(--k-{t})"></span>{label}<span class="n">{byType[t].length}</span></summary>
            {#each byType[t] as c (c.path)}
              <button class="item" class:on={c.path === current} onclick={() => onopen(c.path)} title={c.summary}>
                <span class="t"><span class="dot {isAgent(c) ? 'agent' : 'human'}" title={isAgent(c) ? `Written by ${c.created_by}` : 'Written by a person'}></span>{c.title}</span>
              </button>
            {/each}
          </details>
        {/if}
      {/each}
    {/if}
  </div>
  <footer class="muted">
    <span class="dot agent"></span> agent <span class="dot human"></span> person
    <span class="zip"><button class="quiet" onclick={onexport} title="Save everything as one zip">Export</button><button class="quiet" onclick={onimportzip} title="Merge a zip from another computer">Import</button></span>
  </footer>
</aside>

<style>
  aside { display: flex; flex-direction: column; min-height: 0; background: var(--paper-2); }
  .search { padding: var(--s3) var(--s3) var(--s2); }
  .actions { display: flex; gap: var(--s1); padding: 0 var(--s3) var(--s3); border-bottom: 1px solid var(--rule); }
  .actions button { flex: 1; padding: 3px 4px; font-size: 12px; }
  .list { flex: 1; overflow-y: auto; padding: var(--s2) 0; }
  .pad { padding: 0 var(--s3); }
  details { margin-bottom: var(--s2); }
  summary { display: flex; align-items: center; gap: var(--s2); padding: 3px var(--s3); color: var(--ink-2); font-weight: 600; cursor: pointer; list-style: none; user-select: none; }
  summary::-webkit-details-marker { display: none; }
  .kind { width: 8px; height: 8px; border-radius: 2px; }
  .n { margin-left: auto; font-weight: 400; color: var(--ink-3); font-variant-numeric: tabular-nums; }
  .item { display: block; width: 100%; text-align: left; border: 0; border-radius: 0; padding: 3px var(--s3) 3px 28px; }
  .item.hit { padding-left: var(--s3); padding-block: 6px; }
  .item:hover { background: var(--paper); }
  .item.on { background: var(--paper); box-shadow: inset 2px 0 var(--brass); }
  .t { display: flex; align-items: center; gap: 7px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .s { display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; color: var(--ink-3); font-size: 12px; margin-left: 14px; }
  .zip { margin-left: auto; display: flex; }
  .zip button { font-size: 11px; padding: 1px 6px; }
  footer { display: flex; align-items: center; gap: 6px; padding: var(--s2) var(--s3); border-top: 1px solid var(--rule); font-size: 11px; }
</style>
