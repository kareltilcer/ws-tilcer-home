// Generates the PWA + notification icon set as PNGs from one vector scene.
//
// The scene is the favicon's mark in colour: a house sheltering a man, a woman,
// a cat and a dog. The favicon itself stays black & white (it is read at 16px,
// where colour only muddies it); the notification and launcher icons get the
// colour version, because a notification is where the mark is actually looked
// at — the flat "h" placeholder that used to live here read as an unfinished app.
//
// Run: npm run icons
//
// Rasterised with Playwright's Chromium, already a devDependency for the e2e
// suite. The hand-written PNG encoder this replaced was worth it for four flat
// placeholder tiles; it is not worth it for a scene with curves and gradients.

import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const outDir = resolve(dirname(fileURLToPath(import.meta.url)), '../public/icons')

const C = {
  skyTop: '#8fb6ff',
  skyBottom: '#5f8ff5', // the dark-theme accent, so the tile stays app-coloured
  grass: '#5fb87d',
  roof: '#e2603f',
  roofShade: '#c74e30',
  chimney: '#b8462b',
  wall: '#fff3e0',
  floor: '#f4e4c9',
  skin: '#f6c9a0',
  hairHer: '#4a3226',
  hairHim: '#33241c',
  dress: '#ef5f8b',
  shirt: '#2f9e8f',
  trousers: '#3f5a8a',
  dog: '#e3a44f',
  dogDark: '#c8873a',
  cat: '#8d97a8',
  catDark: '#737f92',
  nose: '#ef8fa5',
  ink: '#3a2a20',
}

/** dog and cat are drawn twice: once with every fill swapped for the wall colour
 *  under a fat stroke, then again in their own colours. The first pass is a halo
 *  that keeps a pet legible against the parent it overlaps; painting the stroke
 *  on the real shapes instead would draw a seam along every internal edge. */
const dog = (halo) => {
  const f = (colour) => (halo ? C.wall : colour)
  return `
      <path d="M10.8 52 L8.6 46.2 L12.2 49.6 Z" fill="${f(C.dog)}"/>
      <rect x="11.2" y="52" width="2" height="6" rx="0.8" fill="${f(C.dog)}"/>
      <rect x="13.8" y="52" width="2" height="6" rx="0.8" fill="${f(C.dog)}"/>
      <rect x="19.6" y="51.5" width="2" height="6.5" rx="0.8" fill="${f(C.dog)}"/>
      <rect x="22.2" y="51.5" width="2" height="6.5" rx="0.8" fill="${f(C.dog)}"/>
      <ellipse cx="15.5" cy="51.6" rx="6.3" ry="3.4" fill="${f(C.dog)}"/>
      <circle cx="22.3" cy="49.6" r="3.4" fill="${f(C.dog)}"/>
      <ellipse cx="25.3" cy="50.9" rx="2.2" ry="1.5" fill="${f(C.dogDark)}"/>
      <path d="M20.9 45.4 L19.2 50.7 L22.4 49.2 Z" fill="${f(C.dogDark)}"/>`
}

const cat = (halo) => {
  const f = (colour) => (halo ? C.wall : colour)
  return `
      <path d="M51.4 53.4 C55.2 52.2 54.6 45 51.9 44.6" fill="none" stroke="${f(C.cat)}"
            stroke-width="${halo ? 3.8 : 2.2}" stroke-linecap="round"/>
      <rect x="40.7" y="53" width="1.8" height="5" rx="0.7" fill="${f(C.cat)}"/>
      <rect x="43" y="53" width="1.8" height="5" rx="0.7" fill="${f(C.cat)}"/>
      <rect x="47.6" y="53.4" width="1.8" height="4.6" rx="0.7" fill="${f(C.cat)}"/>
      <rect x="49.8" y="53.4" width="1.8" height="4.6" rx="0.7" fill="${f(C.cat)}"/>
      <ellipse cx="46.5" cy="53" rx="5.3" ry="3" fill="${f(C.cat)}"/>
      <circle cx="40.8" cy="50.6" r="3.2" fill="${f(C.cat)}"/>
      <path d="M38.2 46.4 L39.6 50.6 L41.2 49 Z" fill="${f(C.catDark)}"/>
      <path d="M43.4 46.4 L42 50.6 L40.4 49 Z" fill="${f(C.catDark)}"/>`
}

/** halo wraps a pet's first pass: same shapes, wall-coloured, fattened. */
const halo = (pet) =>
  `<g stroke="${C.wall}" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round">${pet(true)}</g>`

/** house is the scene's content — everything that must survive a maskable crop.
 *  Drawn back to front: chimney behind the roof, pets in front of the parents. */
const house = `
    <!-- Chimney: drawn first so the roof line cuts off its base. -->
    <rect x="16" y="8.5" width="5.5" height="10" fill="${C.chimney}"/>
    <rect x="14.8" y="7.2" width="7.9" height="2.8" rx="0.8" fill="${C.chimney}"/>

    <!-- Roof, lit on the left and shaded on the right so the gable reads. -->
    <path d="M32 3.5 L63 30 L1 30 Z" fill="${C.roof}"/>
    <path d="M32 3.5 L63 30 L32 30 Z" fill="${C.roofShade}"/>

    <!-- Walls, with the floor the family stands on. -->
    <rect x="7.5" y="30" width="49" height="28.5" fill="${C.wall}"/>
    <rect x="7.5" y="54.5" width="49" height="4" fill="${C.floor}"/>

    <!-- Woman -->
    <circle cx="24" cy="35.4" r="4" fill="${C.hairHer}"/>
    <circle cx="24" cy="36.3" r="3.3" fill="${C.skin}"/>
    <circle cx="22.8" cy="36.2" r="0.45" fill="${C.ink}"/>
    <circle cx="25.2" cy="36.2" r="0.45" fill="${C.ink}"/>
    <path d="M22.9 37.9 Q24 38.8 25.1 37.9" fill="none" stroke="${C.ink}"
          stroke-width="0.4" stroke-linecap="round"/>
    <rect x="21.3" y="56.8" width="2.9" height="1.4" rx="0.5" fill="${C.ink}"/>
    <rect x="24.1" y="56.8" width="2.9" height="1.4" rx="0.5" fill="${C.ink}"/>
    <rect x="21.7" y="52" width="1.9" height="5" fill="${C.skin}"/>
    <rect x="24.5" y="52" width="1.9" height="5" fill="${C.skin}"/>
    <path d="M20.4 41.2 Q24 39 27.6 41.2 L30 53.8 L18 53.8 Z" fill="${C.dress}"/>

    <!-- Man -->
    <circle cx="39" cy="35" r="3.9" fill="${C.hairHim}"/>
    <circle cx="39" cy="35.9" r="3.3" fill="${C.skin}"/>
    <circle cx="37.8" cy="35.8" r="0.45" fill="${C.ink}"/>
    <circle cx="40.2" cy="35.8" r="0.45" fill="${C.ink}"/>
    <path d="M37.9 37.5 Q39 38.4 40.1 37.5" fill="none" stroke="${C.ink}"
          stroke-width="0.4" stroke-linecap="round"/>
    <rect x="34.4" y="56.8" width="4.3" height="1.4" rx="0.5" fill="${C.ink}"/>
    <rect x="39.3" y="56.8" width="4.3" height="1.4" rx="0.5" fill="${C.ink}"/>
    <rect x="34.8" y="48.6" width="3.8" height="8.6" fill="${C.trousers}"/>
    <rect x="39.4" y="48.6" width="3.8" height="8.6" fill="${C.trousers}"/>
    <path d="M34.8 41.4 Q34.8 39.2 39 39.2 Q43.2 39.2 43.2 41.4 L43.2 49.2 L34.8 49.2 Z" fill="${C.shirt}"/>

    <!-- Dog -->
    ${halo(dog)}
    ${dog(false)}
    <circle cx="26.9" cy="50.4" r="0.75" fill="${C.ink}"/>
    <circle cx="22.9" cy="48.9" r="0.6" fill="${C.ink}"/>

    <!-- Cat -->
    ${halo(cat)}
    ${cat(false)}
    <circle cx="39.5" cy="50.2" r="0.6" fill="${C.ink}"/>
    <circle cx="38" cy="51.3" r="0.7" fill="${C.nose}"/>`

/** mark renders the colour tile.
 *  `scale` shrinks the house about the tile's centre for the maskable variant, so
 *  Android's circle/squircle crop bites into sky and grass rather than the roof.
 *  The background is always full-bleed for the same reason. */
function mark(size, { scale = 1, rounded = true } = {}) {
  const grassTop = 32 + (57 - 32) * scale
  const shift = 32 * (1 - scale)
  const clip = rounded
    ? `<clipPath id="tile"><rect width="64" height="64" rx="14"/></clipPath>`
    : ''
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" width="${size}" height="${size}">
  <defs>
    <linearGradient id="sky" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="${C.skyTop}"/>
      <stop offset="1" stop-color="${C.skyBottom}"/>
    </linearGradient>
    ${clip}
  </defs>
  <g${rounded ? ' clip-path="url(#tile)"' : ''}>
    <rect width="64" height="64" fill="url(#sky)"/>
    <rect y="${grassTop}" width="64" height="${64 - grassTop}" fill="${C.grass}"/>
    <g transform="translate(${shift} ${shift}) scale(${scale})">${house}
    </g>
  </g>
</svg>`
}

/** badge renders the Android notification badge: a flat house silhouette, white
 *  on transparent. The platform tints and shrinks it to ~24dp, so the family
 *  cannot come along — anything finer than a door becomes a smudge. */
function badge(size) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 72 72" width="${size}" height="${size}">
  <path fill="#ffffff" fill-rule="evenodd"
        d="M36 6 L68 34 L60 34 L60 62 L12 62 L12 34 L4 34 Z M30 44 L42 44 L42 62 L30 62 Z"/>
</svg>`
}

const assets = [
  ['icon-192.png', 192, mark(192)],
  ['icon-512.png', 512, mark(512)],
  // Maskable: full-bleed, house inside the 80% safe zone.
  ['maskable-512.png', 512, mark(512, { scale: 0.8, rounded: false })],
  ['badge-72.png', 72, badge(72)],
]

mkdirSync(outDir, { recursive: true })

const browser = await chromium.launch()
const page = await browser.newPage()
for (const [name, size, svg] of assets) {
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(
    `<!doctype html><style>html,body{margin:0;padding:0}svg{display:block}</style>${svg}`,
  )
  writeFileSync(resolve(outDir, name), await page.screenshot({ omitBackground: true }))
  console.log('wrote', name, `${size}×${size}`)
}
await browser.close()
