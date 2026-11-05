import MarkdownIt from 'markdown-it'

// html:false — agent-written Markdown must never run script in the app window.
const md = new MarkdownIt({ html: false, linkify: true, typographer: true })

export function stripFrontmatter(s: string): string {
  const m = s.replace(/^﻿/, '').match(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/)
  return m ? s.slice(m[0].length) : s
}

// [[name]] / [[name|label]] → links the viewer resolves by filename.
function wikify(s: string): string {
  return s.replace(/\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|([^\]]+))?\]\]/g, (_, name, label) =>
    `[${(label || name).trim()}](orbis-wiki:${encodeURIComponent(name.trim())})`)
}

md.validateLink = (url) => !/^(javascript|vbscript|file):/i.test(url.trim())

export function render(body: string): string {
  return md.render(wikify(body))
}

/** Split Markdown into slides on lines that are exactly `---`. */
export function slides(body: string): string[] {
  return stripFrontmatter(body).split(/\r?\n---[ \t]*\r?\n/).map((s) => render(s.trim())).filter((s) => s.trim() !== '')
}

/** Resolve a link found in `from` to a vault path, or null if it leaves the vault. */
export function resolvePath(from: string, href: string): string | null {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('#')) return null
  href = decodeURIComponent(href.split('#')[0].split('?')[0])
  if (!href) return null
  const parts = href.startsWith('/') ? [] : from.split('/').slice(0, -1)
  for (const seg of href.split('/')) {
    if (seg === '' || seg === '.') continue
    if (seg === '..') { if (!parts.length) return null; parts.pop() } else parts.push(seg)
  }
  return parts.join('/') || null
}

/** Point relative <img> srcs inside `el` at the vault and mark wikilinks. */
export function fixRefs(el: HTMLElement, from: string) {
  for (const img of el.querySelectorAll('img')) {
    const p = resolvePath(from, img.getAttribute('src') || '')
    if (p) img.src = vaultURL(p)
  }
  for (const a of el.querySelectorAll('a')) {
    if (a.getAttribute('href')?.startsWith('orbis-wiki:')) a.classList.add('wiki')
  }
}

export const vaultURL =(rel: string) => '/vault/' + rel.split('/').map(encodeURIComponent).join('/')
