<script lang="ts">
  import * as api from '../../wailsjs/go/main/App'
  import type { main, vault } from '../../wailsjs/go/models'

  let { doc, onopen, onsaved, ondelete, onerror }: {
    doc: main.Doc; onopen: (p: string) => void; onsaved: () => void; ondelete: (p: string) => void; onerror: (e: string) => void
  } = $props()

  let title = $state(''), summary = $state(''), tags = $state(''), project = $state('')
  let hood = $state<{ card: vault.Card; why: string }[]>([])

  // Reset the form whenever a different file (or a fresh copy of it) arrives.
  $effect(() => {
    const c = doc.card
    title = c.title; summary = c.summary; tags = (c.tags ?? []).join(', '); project = c.project ?? ''
    api.Neighbors(c.path, 1).then((h) => {
      const why = new Map<string, string>()
      for (const e of h.edges ?? []) {
        if (e.kind === 'link') {
          if (e.from === c.path) why.set(e.to, 'Linked from here')
          else if (!why.has(e.from)) why.set(e.from, 'Links here')
        } else if (e.from !== c.path && !why.has(e.from)) {
          const [k, v] = e.to.split(/:(.*)/)
          why.set(e.from, k === 'tag' ? `Shares tag ${v}` : k === 'project' ? `Same project` : 'Same agent session')
        }
      }
      hood = (h.cards ?? []).slice(1).map((card) => ({ card, why: why.get(card.path) ?? '' }))
    }).catch(() => (hood = []))
  })

  const parsedTags = $derived(tags.split(',').map((t) => t.trim()).filter(Boolean))
  const dirty = $derived(title !== doc.card.title || summary !== doc.card.summary ||
    parsedTags.join(',') !== (doc.card.tags ?? []).join(',') || project !== (doc.card.project ?? ''))

  async function save() {
    try {
      await api.SaveCard({ ...doc.card, title, summary, tags: parsedTags, project } as vault.Card)
      onsaved()
    } catch (e) { onerror(String(e)) }
  }

  const agent = $derived(!!doc.card.created_by && doc.card.created_by !== 'human')
  const when = $derived(new Date(doc.card.updated).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }))
  const size = $derived(doc.size < 1024 ? `${doc.size} B` : doc.size < 1 << 20 ? `${(doc.size / 1024).toFixed(1)} KB` : `${(doc.size / (1 << 20)).toFixed(1)} MB`)
</script>

<aside aria-label="Card">
  <form onsubmit={(e) => { e.preventDefault(); save() }}>
    <label>Title<input bind:value={title} /></label>
    <label>Summary <span class="muted">what agents see first</span>
      <textarea bind:value={summary} rows="4" placeholder="One to three sentences on what this is and when it's useful."></textarea>
    </label>
    <label>Tags<input bind:value={tags} placeholder="comma, separated" /></label>
    <label>Project<input bind:value={project} /></label>
    {#if dirty}<button type="submit" class="save">Save card</button>{/if}
  </form>

  <dl>
    <dt>Written by</dt><dd><span class="dot {agent ? 'agent' : 'human'}"></span> {agent ? doc.card.created_by : 'a person'}</dd>
    {#if doc.card.session}<dt>Session</dt><dd class="mono">{doc.card.session}</dd>{/if}
    <dt>Updated</dt><dd>{when}</dd>
    <dt>Size</dt><dd>{size}</dd>
  </dl>

  <h2>Connected</h2>
  {#if hood.length === 0}
    <p class="muted">No links, shared tags or session yet.</p>
  {/if}
  <ul>
    {#each hood as h (h.card.path)}
      <li><button class="quiet" onclick={() => onopen(h.card.path)}>
        <span class="t"><span class="kind" style="background: var(--k-{h.card.type})"></span>{h.card.title}</span>
        <span class="why">{h.why}</span>
      </button></li>
    {/each}
  </ul>

  <button class="quiet danger" onclick={() => ondelete(doc.card.path)}>Delete file</button>
</aside>

<style>
  aside { width: 300px; border-left: 1px solid var(--rule); padding: var(--s4); overflow-y: auto; display: flex; flex-direction: column; gap: var(--s4); }
  form { display: flex; flex-direction: column; gap: var(--s3); }
  label { display: flex; flex-direction: column; gap: var(--s1); font-weight: 600; color: var(--ink-2); font-size: 12px; }
  label .muted { font-weight: 400; display: inline; }
  label input, label textarea { font-weight: 400; color: var(--ink); font-size: 13px; }
  textarea { resize: vertical; line-height: 1.45; }
  .save { align-self: flex-start; border-color: var(--brass); color: var(--brass); }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px var(--s3); margin: 0; font-size: 12px; }
  dt { color: var(--ink-3); }
  dd { margin: 0; display: flex; align-items: center; gap: 6px; min-width: 0; overflow-wrap: anywhere; }
  .mono { font-family: var(--mono); font-size: 11px; }
  h2 { font-size: 12px; margin: 0; color: var(--ink-2); }
  ul { list-style: none; margin: calc(-1 * var(--s2)) 0 0; padding: 0; }
  li button { display: block; width: 100%; text-align: left; padding: 5px 6px; }
  .t { display: flex; align-items: center; gap: 6px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .kind { width: 7px; height: 7px; border-radius: 2px; flex: none; }
  .why { display: block; font-size: 11px; color: var(--ink-3); margin-left: 13px; }
  .danger { color: var(--danger); align-self: flex-start; margin-top: auto; }
  @media (max-width: 900px) { aside { display: none; } }
</style>
