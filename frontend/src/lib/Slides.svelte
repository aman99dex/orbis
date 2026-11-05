<script lang="ts">
  import { onMount } from 'svelte'
  import type { main } from '../../wailsjs/go/models'
  import { slides, vaultURL, fixRefs } from './md'

  let { doc, onclose }: { doc: main.Doc; onclose: () => void } = $props()

  const isHTML = $derived(/\.html?$/i.test(doc.card.path))
  const deck = $derived(isHTML ? [] : slides(doc.content))
  let i = $state(0)
  let el: HTMLDivElement
  let slideEl = $state<HTMLDivElement>()
  $effect(() => { void i; void deck; if (slideEl) fixRefs(slideEl, doc.card.path) })

  onMount(() => {
    el.requestFullscreen?.().catch(() => {})
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
      else if (['ArrowRight', 'ArrowDown', 'PageDown', ' '].includes(e.key)) { e.preventDefault(); i = Math.min(i + 1, deck.length - 1) }
      else if (['ArrowLeft', 'ArrowUp', 'PageUp'].includes(e.key)) { e.preventDefault(); i = Math.max(i - 1, 0) }
      else if (e.key === 'Home') i = 0
      else if (e.key === 'End') i = deck.length - 1
    }
    const fs = () => { if (!document.fullscreenElement) onclose() }
    window.addEventListener('keydown', key)
    document.addEventListener('fullscreenchange', fs)
    return () => { window.removeEventListener('keydown', key); document.removeEventListener('fullscreenchange', fs) }
  })

  function close() {
    if (document.fullscreenElement) document.exitFullscreen()
    else onclose()
  }
</script>

<div class="deck" bind:this={el} role="dialog" aria-label="Presentation">
  {#if isHTML}
    <iframe title={doc.card.title} src={vaultURL(doc.card.path)} sandbox="allow-scripts allow-popups allow-forms allow-modals"></iframe>
  {:else}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="slide prose" bind:this={slideEl} onclick={() => (i = Math.min(i + 1, deck.length - 1))}>{@html deck[i] ?? ''}</div>
    <div class="progress" style="width: {deck.length ? ((i + 1) / deck.length) * 100 : 0}%"></div>
    <span class="count">{i + 1} / {deck.length}</span>
  {/if}
  <button class="quiet close" onclick={close}>Exit <kbd>Esc</kbd></button>
</div>

<style>
  .deck { position: fixed; inset: 0; z-index: 10; background: var(--paper); display: grid; place-items: center; overflow-y: auto; }
  iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; background: #fff; }
  .slide { font-size: clamp(20px, 2.4vw, 34px); max-width: 30em; width: 100%; padding: 6vh 6vw; }
  .slide :global(h1) { font-size: 2em; }
  .slide :global(img) { max-height: 60vh; }
  .progress { position: absolute; left: 0; bottom: 0; height: 3px; background: var(--brass); transition: width .2s; }
  .count { position: absolute; right: var(--s5); bottom: var(--s4); color: var(--ink-3); font-variant-numeric: tabular-nums; }
  .close { position: absolute; top: var(--s3); right: var(--s3); opacity: .6; }
  .close:hover { opacity: 1; }
</style>
