<script lang="ts">
  import type { main } from '../../wailsjs/go/models'
  import { render, stripFrontmatter, resolvePath, vaultURL, fixRefs } from './md'

  let { doc, onopen, onwiki, onexternal }: {
    doc: main.Doc; onopen: (p: string) => void; onwiki: (name: string) => void; onexternal: (url: string) => void
  } = $props()

  const path = $derived(doc.card.path)
  const ext = $derived(path.split('.').pop()!.toLowerCase())
  const kind = $derived(
    ['md', 'markdown'].includes(ext) ? 'md'
    : ['html', 'htm'].includes(ext) ? 'html'
    : ext === 'pdf' ? 'pdf'
    : ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'avif'].includes(ext) ? 'image'
    : doc.text ? 'text' : 'binary')
  const html = $derived(kind === 'md' ? render(stripFrontmatter(doc.content)) : '')

  let prose = $state<HTMLElement>()
  $effect(() => { void html; if (prose) fixRefs(prose, path) })

  function click(e: MouseEvent) {
    const a = (e.target as HTMLElement).closest('a')
    if (!a) return
    e.preventDefault()
    const href = a.getAttribute('href') || ''
    if (href.startsWith('orbis-wiki:')) onwiki(decodeURIComponent(href.slice(11)))
    else if (/^https?:/i.test(href)) onexternal(href)
    else { const p = resolvePath(path, href); if (p) onopen(p) }
  }
</script>

{#if kind === 'md'}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
  <article class="prose" bind:this={prose} onclick={click}>{@html html}</article>
{:else if kind === 'html'}
  <iframe class="fill" title={doc.card.title} src={vaultURL(path)} sandbox="allow-scripts allow-popups allow-forms allow-modals"></iframe>
{:else if kind === 'pdf'}
  <iframe class="fill" title={doc.card.title} src={vaultURL(path)}></iframe>
{:else if kind === 'image'}
  <div class="img"><img src={vaultURL(path)} alt={doc.card.summary || doc.card.title} /></div>
{:else if kind === 'text'}
  <pre class="text">{doc.content}</pre>
{:else}
  <div class="bin">
    <p>Orbis can't show this kind of file yet. It's stored at</p>
    <pre>{path}</pre>
  </div>
{/if}

<style>
  .fill { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; background: #fff; }
  .img { display: grid; place-items: center; min-height: 100%; padding: var(--s5); }
  .img img { max-width: 100%; max-height: calc(100vh - 120px); object-fit: contain; }
  .text { margin: 0; padding: var(--s5); font: 13px/1.55 var(--mono); white-space: pre-wrap; word-break: break-word; }
  .bin { padding: var(--s6); color: var(--ink-2); }
  .bin pre { font: 12px var(--mono); }
</style>
