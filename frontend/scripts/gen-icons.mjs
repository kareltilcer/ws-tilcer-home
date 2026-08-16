// Generates the PWA + notification icon set as PNGs.
//
// These are PLACEHOLDERS. HANDOFF-7 §14 lists the real assets (maskable 192/512
// plus an Android mono badge) as still owed by design; until they land, shipping
// the app's own "h" mark on the accent colour is better than shipping a manifest
// that points at nothing — an installed PWA with a missing icon looks broken in a
// way a plain mark does not.
//
// Run: node scripts/gen-icons.mjs
//
// Written by hand rather than with a rasteriser so the repo gains no image
// dependency for four flat images.

import { deflateSync } from 'node:zlib'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const outDir = resolve(dirname(fileURLToPath(import.meta.url)), '../public/icons')

// The dark-theme accent and its foreground, matching theme/globals.css.
const ACCENT = [0x6e, 0x9e, 0xff, 0xff]
const FG = [0x0f, 0x11, 0x15, 0xff]
const TRANSPARENT = [0, 0, 0, 0]
const WHITE = [0xff, 0xff, 0xff, 0xff]

/** crc32 over a byte array (PNG chunk checksums). */
function crc32(buf) {
  let c = ~0
  for (const b of buf) {
    c ^= b
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1))
  }
  return ~c >>> 0
}

function chunk(type, data) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body))
  return Buffer.concat([len, body, crc])
}

/** png encodes RGBA pixels (a size*size*4 buffer) as a PNG. */
function png(size, pixels) {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(size, 0)
  ihdr.writeUInt32BE(size, 4)
  ihdr[8] = 8 // bit depth
  ihdr[9] = 6 // colour type: RGBA
  // 10-12: compression, filter, interlace — all 0.

  // Each scanline is prefixed with filter type 0 (None).
  const raw = Buffer.alloc(size * (size * 4 + 1))
  for (let y = 0; y < size; y++) {
    const rowStart = y * (size * 4 + 1)
    raw[rowStart] = 0
    pixels.copy(raw, rowStart + 1, y * size * 4, (y + 1) * size * 4)
  }

  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

/** draw renders the mark: a rounded accent tile with a blocky lowercase "h". */
function draw(size, { background, glyph, padding = 0, rounded = true }) {
  const px = Buffer.alloc(size * size * 4)
  const put = (x, y, colour) => {
    if (x < 0 || y < 0 || x >= size || y >= size) return
    const i = (y * size + x) * 4
    px[i] = colour[0]
    px[i + 1] = colour[1]
    px[i + 2] = colour[2]
    px[i + 3] = colour[3]
  }

  // Tile (rounded square, or full-bleed for maskable so the platform can crop).
  const radius = rounded ? Math.round(size * 0.22) : 0
  const inset = Math.round(size * padding)
  for (let y = inset; y < size - inset; y++) {
    for (let x = inset; x < size - inset; x++) {
      if (rounded) {
        const dx = Math.min(x - inset, size - 1 - inset - x)
        const dy = Math.min(y - inset, size - 1 - inset - y)
        if (dx < radius && dy < radius) {
          const cx = radius - dx
          const cy = radius - dy
          if (cx * cx + cy * cy > radius * radius) continue
        }
      }
      put(x, y, background)
    }
  }

  // A blocky "h": left stem full height, shoulder, right stem from mid-height.
  const unit = size / 16
  const stemW = Math.round(unit * 2)
  const left = Math.round(unit * 5)
  const right = Math.round(unit * 9)
  const top = Math.round(unit * 3.5)
  const bottom = Math.round(unit * 12.5)
  const shoulder = Math.round(unit * 7.5)

  const rect = (x0, y0, x1, y1) => {
    for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) put(x, y, glyph)
  }
  rect(left, top, left + stemW, bottom) // left stem
  rect(left, shoulder, right + stemW, shoulder + stemW) // shoulder
  rect(right, shoulder, right + stemW, bottom) // right stem

  return px
}

mkdirSync(outDir, { recursive: true })

const assets = [
  // Standard launcher icons.
  ['icon-192.png', 192, { background: ACCENT, glyph: FG }],
  ['icon-512.png', 512, { background: ACCENT, glyph: FG }],
  // Maskable: full-bleed with the glyph inside the safe zone, so Android's
  // circle/squircle crop never clips the mark.
  ['maskable-512.png', 512, { background: ACCENT, glyph: FG, rounded: false }],
  // Android notification badge: monochrome on transparent — the platform tints it.
  ['badge-72.png', 72, { background: TRANSPARENT, glyph: WHITE, rounded: false }],
]

for (const [name, size, opts] of assets) {
  writeFileSync(resolve(outDir, name), png(size, draw(size, opts)))
  console.log('wrote', name, `${size}×${size}`)
}
