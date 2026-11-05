<script lang="ts">
  import { onMount } from 'svelte'
  import Graph from 'graphology'
  import Sigma from 'sigma'
  import forceAtlas2 from 'graphology-layout-forceatlas2'
  import * as api from '../../wailsjs/go/main/App'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import type { main } from '../../wailsjs/go/models'

  let { focus, onopen }: { focus: string | null; onopen: (p: string) => void } = $props()

  const FILE_KINDS = ['note', 'page', 'artifact', 'pdf', 'image', 'file']
  let data = $state<main.GraphData | null>(null)
  let center = $state<string | null>(null)
  let depth = $state(2)
  let hubs = $state(true)
  let rings = $state<{ cx: number; cy: number; r: number[] }>({ cx: 0, cy: 0, r: [] })
  let shown = $state(0)
  let container: HTMLDivElement
  let renderer: Sigma | null = null

  $effect(() => { center = focus })

  const css = (v: string) => getComputedStyle(document.documentElement).getPropertyValue(v).trim()
  const isFile = (kind: string) => FILE_KINDS.includes(kind)

  function build(d: main.GraphData, c: string | null, maxDepth: number, withHubs: boolean) {
    const g = new Graph({ type: 'undirected' })
    for (const n of d.nodes ?? []) {
      if (!withHubs && !isFile(n.kind)) continue
      g.addNode(n.id, {
        label: isFile(n.kind) ? n.label : n.kind === 'tag' ? `#${n.label}` : n.label,
        size: isFile(n.kind) ? 6 : 3.5,
        color: css(isFile(n.kind) ? `--k-${n.kind}` : '--k-hub'),
        kind: n.kind,
      })
    }
    for (const e of d.edges ?? []) {
      if (g.hasNode(e.from) && g.hasNode(e.to) && !g.hasEdge(e.from, e.to)) g.addEdge(e.from, e.to, { size: e.kind === 'link' ? 1.2 : 0.6 })
    }

    if (c && g.hasNode(c)) {
      // Orbit layout: the focus at the centre, each hop on its own ring.
      const hop = new Map<string, number>([[c, 0]])
      const parent = new Map<string, string>()
      let frontier = [c]
      for (let k = 1; k <= maxDepth && frontier.length; k++) {
        const next: string[] = []
        for (const f of frontier) g.forEachNeighbor(f, (nb) => {
          if (!hop.has(nb)) { hop.set(nb, k); parent.set(nb, f); next.push(nb) }
        })
        frontier = next
      }
      g.forEachNode((n) => { if (!hop.has(n)) g.dropNode(n) })
      const angle = new Map<string, number>([[c, 0]])
      g.setNodeAttribute(c, 'x', 0); g.setNodeAttribute(c, 'y', 0); g.setNodeAttribute(c, 'size', 11)
      for (let k = 1; k <= maxDepth; k++) {
        const ring = [...hop].filter(([, h]) => h === k).map(([n]) => n)
          .sort((a, b) => angle.get(parent.get(a)!)! - angle.get(parent.get(b)!)! || a.localeCompare(b))
        ring.forEach((n, i) => {
          const t = (2 * Math.PI * i) / ring.length + k * 0.35 // offset each ring so spokes don't line up
          angle.set(n, t)
          g.setNodeAttribute(n, 'x', k * Math.cos(t)); g.setNodeAttribute(n, 'y', k * Math.sin(t))
        })
      }
    } else {
      g.forEachNode((n) => { g.setNodeAttribute(n, 'x', Math.random()); g.setNodeAttribute(n, 'y', Math.random()) })
      // ponytail: synchronous layout; move to the FA2 web worker if 10k-node vaults stutter on open
      if (g.order > 1) forceAtlas2.assign(g, { iterations: g.order > 3000 ? 60 : 200, settings: { ...forceAtlas2.inferSettings(g), barnesHutOptimize: g.order > 500 } })
    }
    return g
  }

  function draw() {
    renderer?.kill()
    renderer = null
    if (!data || !container) return
    const g = build(data, center, depth, hubs)
    shown = g.order
    const orbit = !!center && g.hasNode(center)
    renderer = new Sigma(g, container, {
      labelFont: css('--ui'), labelSize: 12, labelColor: { color: css('--ink') },
      defaultEdgeColor: css('--rule'), labelRenderedSizeThreshold: orbit || g.order < 300 ? 0 : 7, zIndex: true,
      stagePadding: 64, // keep the outer ring clear of the control bar
    })
    // In orbit, spokes from the centre read stronger than edges between neighbours.
    if (orbit) g.forEachEdge(center!, (e) => g.setEdgeAttribute(e, 'color', css('--ink-3')))
    renderer.on('clickNode', ({ node }) => {
      if (node === center && isFile(g.getNodeAttribute(node, 'kind'))) onopen(node)
      else center = node
    })
    renderer.on('doubleClickNode', ({ node, preventSigmaDefault }) => {
      preventSigmaDefault()
      if (isFile(g.getNodeAttribute(node, 'kind'))) onopen(node)
    })
    renderer.on('afterRender', () => {
      if (!orbit || !renderer) { rings = { cx: 0, cy: 0, r: [] }; return }
      const o = renderer.graphToViewport({ x: 0, y: 0 })
      const r: number[] = []
      for (let k = 1; k <= depth; k++) { const p = renderer.graphToViewport({ x: k, y: 0 }); r.push(Math.hypot(p.x - o.x, p.y - o.y)) }
      rings = { cx: o.x, cy: o.y, r }
    })
  }

  $effect(() => { void [data, center, depth, hubs]; draw() })

  async function load() { data = await api.Graph() }
  onMount(() => {
    load()
    const off = EventsOn('vault:changed', load)
    return () => { off(); renderer?.kill() }
  })

  const centerLabel = $derived(data?.nodes?.find((n) => n.id === center))
</script>

<div class="graph">
  <div class="canvas" bind:this={container}></div>
  <svg class="rings" aria-hidden="true">
    {#each rings.r as r}<circle cx={rings.cx} cy={rings.cy} {r} />{/each}
  </svg>

  <div class="bar">
    {#if center && centerLabel}
      <span class="focus"><span class="dot agent"></span>{centerLabel.label}</span>
      <label>Rings <select bind:value={depth}>{#each [1, 2, 3, 4] as d}<option value={d}>{d}</option>{/each}</select></label>
      {#if isFile(centerLabel.kind)}<button onclick={() => onopen(center!)}>Open</button>{/if}
      <button onclick={() => (center = null)}>Show everything</button>
    {:else}
      <span class="muted">{shown} nodes. Click one to put it in orbit.</span>
    {/if}
    <label class="check"><input type="checkbox" bind:checked={hubs} /> Tags, projects and sessions</label>
  </div>
  {#if data && (data.nodes ?? []).length === 0}
    <p class="empty muted">The orbit fills in as you and your agents add files.</p>
  {/if}
</div>

<style>
  .graph { position: absolute; inset: 0; }
  .canvas, .rings { position: absolute; inset: 0; width: 100%; height: 100%; }
  .rings { pointer-events: none; }
  .rings circle { fill: none; stroke: var(--brass); stroke-opacity: .35; stroke-dasharray: 2 5; }
  .bar { position: absolute; left: var(--s3); right: var(--s3); bottom: var(--s3); display: flex; flex-wrap: wrap; align-items: center; gap: var(--s3);
    padding: var(--s2) var(--s3); background: var(--paper-2); border: 1px solid var(--rule); border-radius: 8px; }
  .focus { display: flex; align-items: center; gap: 6px; font-weight: 600; max-width: 40ch; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  label { display: flex; align-items: center; gap: 6px; color: var(--ink-2); }
  select { background: var(--paper); border: 1px solid var(--rule); border-radius: 6px; padding: 2px 4px; }
  .check { margin-left: auto; }
  .check input { width: auto; }
  .empty { position: absolute; top: 40%; width: 100%; text-align: center; }
</style>
