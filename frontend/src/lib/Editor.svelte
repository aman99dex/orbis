<script lang="ts">
  import { onMount } from 'svelte'
  import { EditorView, basicSetup } from 'codemirror'
  import { keymap } from '@codemirror/view'
  import { markdown } from '@codemirror/lang-markdown'
  import { html } from '@codemirror/lang-html'
  import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { tags as t } from '@lezer/highlight'

  // Token colours from the app palette so both themes stay readable.
  const orbisHighlight = HighlightStyle.define([
    { tag: t.heading, fontWeight: '700', color: 'var(--ink)' },
    { tag: [t.strong], fontWeight: '700' },
    { tag: [t.emphasis], fontStyle: 'italic' },
    { tag: [t.link, t.url], color: 'var(--k-note)' },
    { tag: [t.processingInstruction, t.meta, t.contentSeparator, t.punctuation, t.angleBracket], color: 'var(--ink-3)' },
    { tag: [t.monospace], color: 'var(--k-artifact)' },
    { tag: [t.quote], color: 'var(--ink-2)', fontStyle: 'italic' },
    { tag: [t.tagName], color: 'var(--k-note)' },
    { tag: [t.attributeName], color: 'var(--k-page)' },
    { tag: [t.attributeValue, t.string], color: 'var(--k-pdf)' },
    { tag: [t.comment], color: 'var(--ink-3)', fontStyle: 'italic' },
    { tag: [t.keyword, t.list], color: 'var(--brass)' },
  ])
  import * as api from '../../wailsjs/go/main/App'
  import type { main } from '../../wailsjs/go/models'

  let { doc, onsaved, onerror }: { doc: main.Doc; onsaved: () => void; onerror: (e: string) => void } = $props()

  let host: HTMLDivElement
  let status = $state<'saved' | 'unsaved' | 'saving'>('saved')
  let timer: ReturnType<typeof setTimeout>
  let view: EditorView

  async function save() {
    clearTimeout(timer)
    if (status === 'saved') return
    status = 'saving'
    try {
      await api.SaveContent(doc.card.path, view.state.doc.toString())
      status = 'saved'
      onsaved()
    } catch (e) { status = 'unsaved'; onerror(String(e)) }
  }

  onMount(() => {
    const p = doc.card.path.toLowerCase()
    view = new EditorView({
      parent: host,
      doc: doc.content,
      extensions: [
        basicSetup,
        syntaxHighlighting(orbisHighlight),
        EditorView.lineWrapping,
        p.endsWith('.md') || p.endsWith('.markdown') ? markdown() : /\.(html?|svg)$/.test(p) ? html() : [],
        keymap.of([{ key: 'Mod-s', run: () => { save(); return true } }]),
        EditorView.updateListener.of((u) => {
          if (!u.docChanged) return
          status = 'unsaved'
          clearTimeout(timer)
          timer = setTimeout(save, 700) // autosave after a short pause
        }),
        EditorView.theme({
          '&': { height: '100%', background: 'var(--paper)', color: 'var(--ink)' },
          '.cm-scroller': { fontFamily: 'var(--mono)', fontSize: '13.5px', lineHeight: '1.6' },
          '.cm-content': { maxWidth: '90ch', margin: '0 auto', padding: '24px 0 120px' },
          '.cm-gutters': { background: 'var(--paper)', border: 'none', color: 'var(--ink-3)' },
          '.cm-activeLine, .cm-activeLineGutter': { background: 'var(--brass-soft)' },
          '&.cm-focused': { outline: 'none' },
          '.cm-cursor': { borderLeftColor: 'var(--brass)' },
          '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': { background: 'var(--brass-soft) !important' },
        }),
      ],
    })
    view.focus()
    return () => { if (status === 'unsaved') save(); view.destroy() }
  })
</script>

<div class="wrap">
  <div class="host" bind:this={host}></div>
  <span class="status" aria-live="polite">{status === 'saved' ? 'Saved' : status === 'saving' ? 'Saving…' : 'Unsaved'}</span>
</div>

<style>
  .wrap { position: absolute; inset: 0; }
  .host { height: 100%; }
  .status { position: absolute; right: var(--s4); bottom: var(--s3); font-size: 11px; color: var(--ink-3); background: var(--paper); padding: 2px 6px; border-radius: 4px; }
</style>
