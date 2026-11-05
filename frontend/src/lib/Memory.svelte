<script lang="ts">
  import { onMount } from 'svelte'
  import * as api from '../../wailsjs/go/main/App'

  let { mode }: { mode: string } = $props()

  let briefing = $state('')
  let loading = $state(true)
  let q = $state('')
  let answer = $state('')
  let asking = $state(false)
  let error = $state('')

  onMount(async () => {
    try { briefing = await api.MemoryContext() } catch (e) { error = String(e) }
    loading = false
  })

  async function ask(e: SubmitEvent) {
    e.preventDefault()
    if (!q.trim()) return
    asking = true; error = ''; answer = ''
    try { answer = await api.Recall(q.trim()) } catch (err) { error = String(err) }
    asking = false
  }
</script>

<div class="mem">
  <h1>What your agents remember</h1>
  <p class="muted">From Agentic Memory ({mode === 'local' ? 'local, on this computer' : 'memory server'}). This is the briefing a new agent session starts with.</p>

  <form onsubmit={ask}>
    <input bind:value={q} placeholder="Ask your memory, e.g. what did we decide about the gateway?" aria-label="Ask your memory" />
    <button type="submit" disabled={asking}>{asking ? 'Recalling…' : 'Recall'}</button>
  </form>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if answer}<section class="answer" aria-live="polite"><h2>Recalled</h2><div class="text">{answer}</div></section>{/if}

  <section>
    <h2>Briefing</h2>
    {#if loading}<p class="muted">Loading… the first run can take a minute while the embedding model loads.</p>
    {:else}<div class="text">{briefing}</div>{/if}
  </section>
</div>

<style>
  .mem { max-width: 72ch; margin: 0 auto; padding: var(--s6) var(--s5) 120px; }
  h1 { font: 600 26px/1.2 var(--read); margin: 0 0 var(--s2); }
  h2 { font-size: 12px; color: var(--ink-2); margin: var(--s5) 0 var(--s2); }
  form { display: flex; gap: var(--s2); margin-top: var(--s4); }
  form button { flex: none; }
  .text { font: 15px/1.6 var(--read); white-space: pre-wrap; overflow-wrap: anywhere; }
  .answer .text { border-left: 3px solid var(--brass); padding-left: var(--s4); }
  .error { color: var(--danger); white-space: pre-wrap; }
</style>
