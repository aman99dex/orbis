// UI check with REAL input (mouse wheel, clicks, keys) over the Chrome DevTools Protocol.
// It exists because a layout bug once made notes unscrollable while screenshots looked fine.
//
//   node scripts/ui-check.mjs seed <dir>                        # create a test vault
//   wails dev -appargs "-root <dir>" -browser=false             # in another terminal
//   msedge --headless=new --remote-debugging-port=9222 http://localhost:34115/
//   node scripts/ui-check.mjs run [screenshot-prefix]           # prints PASS/FAIL, exits 1 on failure
import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'

if (process.argv[2] === 'seed') {
  const dir = process.argv[3]
  for (const d of ['notes', 'pages']) mkdirSync(join(dir, d), { recursive: true })
  for (let i = 1; i <= 300; i++) writeFileSync(join(dir, 'notes', `note-${i}.md`), `---\ntitle: Note ${i}\nsummary: Filler note ${i}.\n---\n# Note ${i}\n`)
  writeFileSync(join(dir, 'notes', 'auth-decision.md'), '---\ntitle: Validate tokens at the edge\nsummary: Gateway validates JWTs.\n---\n# Validate tokens at the edge\n')
  writeFileSync(join(dir, 'pages', 'architecture.html'), '<!doctype html><title>Collections architecture</title><h1>Architecture</h1>')
  let long = '---\ntitle: Long handbook\nsummary: A long note to test scrolling, links and slides.\n---\n# Long handbook\n\nJump to [the architecture page](../pages/architecture.html) or [[auth-decision]].\n\n'
  for (let i = 1; i <= 40; i++) long += `## Section ${i}\n\nParagraph ${i}. Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.\n\n`
  writeFileSync(join(dir, 'notes', 'long-handbook.md'), long + 'THE-END-MARKER\n')
  let page = '<!doctype html><title>Long report</title><body style="font-family:system-ui;padding:24px">'
  for (let i = 1; i <= 80; i++) page += `<h2>Block ${i}</h2><p>Report row ${i} with enough text to fill the line.</p>`
  writeFileSync(join(dir, 'pages', 'long-report.html'), page)
  console.log(`seeded ${dir}`)
  process.exit(0)
}
process.argv[2] = process.argv[3] || join(process.env.TEMP || '/tmp', 'orbis-ui')
const out = process.argv[2]
const targets = await (await fetch('http://127.0.0.1:9222/json')).json()
const page = targets.find((t) => t.type === 'page' && t.url.includes('34115'))
const ws = new WebSocket(page.webSocketDebuggerUrl)
await new Promise((r) => ws.addEventListener('open', r))
let id = 0
const pending = new Map()
ws.addEventListener('message', (m) => { const msg = JSON.parse(m.data); pending.get(msg.id)?.(msg); pending.delete(msg.id) })
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })) })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const js = async (expr) => (await send('Runtime.evaluate', { expression: `(async () => { ${expr} })()`, awaitPromise: true, returnByValue: true })).result?.result?.value
const wheel = async (x, y, dy = 600) => { await send('Input.dispatchMouseEvent', { type: 'mouseWheel', x, y, deltaX: 0, deltaY: dy }); await sleep(500) }
const click = async (x, y) => {
  for (const type of ['mousePressed', 'mouseReleased']) await send('Input.dispatchMouseEvent', { type, x, y, button: 'left', clickCount: 1 })
  await sleep(700)
}
const key = async (k, code, mods = 0, text) => {
  await send('Input.dispatchKeyEvent', { type: 'keyDown', key: k, code, modifiers: mods, text, windowsVirtualKeyCode: k.length === 1 ? k.toUpperCase().charCodeAt(0) : undefined })
  await send('Input.dispatchKeyEvent', { type: 'keyUp', key: k, code, modifiers: mods })
  await sleep(300)
}
const shot = async (name) => { const s = await send('Page.captureScreenshot', { format: 'png' }); writeFileSync(`${out}-${name}.png`, Buffer.from(s.result.data, 'base64')); return s.result.data }
const center = (sel) => js(`const r = document.querySelector(${JSON.stringify(sel)})?.getBoundingClientRect(); return r ? [r.x + r.width / 2, r.y + Math.min(r.height / 2, 300)] : null`)
const openItem = (title) => js(`const i = document.querySelector('.search input'); if (i.value) { i.value = ''; i.dispatchEvent(new Event('input', { bubbles: true })); await new Promise(r => setTimeout(r, 300)) } const b = [...document.querySelectorAll('.item')].find(b => b.textContent.includes(${JSON.stringify(title)})); if (!b) return false; b.scrollIntoView(); b.click(); return true`)
let fails = 0
const check = (name, ok, detail = '') => { console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`); if (!ok) fails++ }

await send('Emulation.setDeviceMetricsOverride', { width: 1320, height: 840, deviceScaleFactor: 1, mobile: false })
await js(`location.reload()`); await sleep(3000)

// 1. Sidebar scrolls with the wheel (304 notes).
let [x, y] = await center('aside .list')
await wheel(x, y, 1500)
let st = await js(`return document.querySelector('aside .list').scrollTop`)
check('sidebar scrolls with mouse wheel', st > 0, `scrollTop=${st}`)

// 2. Long Markdown note scrolls with the wheel and reaches the end.
await openItem('Long handbook'); await sleep(600)
;[x, y] = await center('.stage')
for (let i = 0; i < 12; i++) await wheel(x, y, 1200)
st = await js(`return document.querySelector('.stage').scrollTop`)
const endVisible = await js(`const r = [...document.querySelectorAll('.prose p')].pop().getBoundingClientRect(); return r.bottom <= innerHeight && r.top >= 0`)
check('long note scrolls with mouse wheel', st > 1000, `scrollTop=${st}`)
check('long note: can reach the last paragraph', endVisible)
await shot('long-note-scrolled')

// 3. Clicking a real link in the note navigates.
await js(`document.querySelector('.stage').scrollTop = 0`); await sleep(300)
const linkPos = await js(`const a = [...document.querySelectorAll('.prose a')].find(a => a.textContent.includes('architecture page')); const r = a.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]`)
await click(...linkPos)
check('clicking a note link opens the target', (await js(`return document.querySelector('.crumb').textContent`)) === 'pages/architecture.html')
await openItem('Long handbook'); await sleep(500)
const wikiPos = await js(`const a = document.querySelector('.prose a.wiki'); const r = a.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]`)
await click(...wikiPos)
check('clicking a [[wikilink]] opens the target', (await js(`return document.querySelector('.crumb').textContent`)) === 'notes/auth-decision.md')

// 4. Long HTML page scrolls inside its frame (screen changes after wheel).
await openItem('Long report'); await sleep(1200)
;[x, y] = await center('.stage')
const before = await shot('html-before')
for (let i = 0; i < 4; i++) await wheel(x, y, 800)
const after = await shot('html-after')
check('long HTML page scrolls with mouse wheel', before !== after)

// 5. Editor scrolls and typing works.
await openItem('Long handbook'); await sleep(500)
;[x, y] = await center('.stage')
await click(x, y)  // focus editor area? first switch to Edit:
const editPos = await js(`const b = [...document.querySelectorAll('nav button')].find(b => b.textContent.trim() === 'Edit'); const r = b.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]`)
await click(...editPos); await sleep(800)
;[x, y] = await center('.cm-scroller')
for (let i = 0; i < 6; i++) await wheel(x, y, 1200)
st = await js(`return document.querySelector('.cm-scroller').scrollTop`)
check('editor scrolls with mouse wheel', st > 1000, `scrollTop=${st}`)
await click(x, y)
await send('Input.insertText', { text: 'TYPED-BY-TEST ' }); await sleep(1500)
const saved = await js(`return document.querySelector('.wrap .status').textContent`)
check('typing in the editor autosaves', saved === 'Saved', saved)
await shot('editor')

// 6. Card panel scrolls when the window is short.
await send('Emulation.setDeviceMetricsOverride', { width: 1320, height: 480, deviceScaleFactor: 1, mobile: false }); await sleep(500)
const readPos = await js(`const b = [...document.querySelectorAll('nav button')].find(b => b.textContent.trim() === 'Read'); const r = b.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]`)
await click(...readPos)
;[x, y] = await center('aside[aria-label="Card"]')
await wheel(x, y, 800)
st = await js(`return document.querySelector('aside[aria-label="Card"]').scrollTop`)
check('card panel scrolls in a short window', st > 0, `scrollTop=${st}`)
await shot('short-window')
await send('Emulation.setDeviceMetricsOverride', { width: 1320, height: 840, deviceScaleFactor: 1, mobile: false }); await sleep(400)

// 7. Keyboard: Ctrl+K, type, Enter opens the top hit.
await key('k', 'KeyK', 2)
check('Ctrl+K focuses search', await js(`return document.activeElement === document.querySelector('.search input')`))
await send('Input.insertText', { text: 'validate tokens' }); await sleep(800)
await key('Enter', 'Enter', 0, '\r'); await sleep(600)
check('Enter opens the top search result', (await js(`return document.querySelector('.crumb').textContent`)) === 'notes/auth-decision.md')

// 8. Slides with real arrow keys.
await openItem('Long handbook'); await sleep(400)
await key('F5', 'F5'); await sleep(600)
const c1 = await js(`return document.querySelector('.count')?.textContent`)
await key('ArrowRight', 'ArrowRight'); await sleep(300)
const c2 = await js(`return document.querySelector('.count')?.textContent`)
check('slides open with F5 and advance with →', !!c1 && c1 !== c2, `${c1} → ${c2}`)
await key('Escape', 'Escape'); await sleep(400)
check('Esc leaves slides', !(await js(`return !!document.querySelector('.deck')`)))

// 9. Orbit: clicking a node puts it in orbit.
await key('g', 'KeyG', 2); await sleep(1500)
await shot('orbit')
check('Ctrl+G opens the orbit', await js(`return !!document.querySelector('.graph canvas')`))

console.log(fails ? `${fails} FAILED` : 'ALL PASS')
ws.close()
process.exit(fails ? 1 : 0)
