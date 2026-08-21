import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'

// VT AND NT MUST DIFFER BY MORE THAN COLOUR.
//
// They are two chart series aliased onto the Path A palette (--el-vt = --c1,
// --el-nt = --c2), and they sit EDGE-TO-EDGE in the share bar and the stacked
// columns — which is precisely the adjacency case the v6 palette work flagged.
// So the encoding is threefold and every one of them is load-bearing:
//
//   SHAPE   — VT is a square swatch, NT a circle.
//   TEXTURE — NT carries a diagonal hatch; VT is a solid fill.
//   LABEL   — both are always named in text, never left to the swatch alone.
//
// The hatch is drawn with repeating-linear-gradient at low contrast, so it reads
// as a texture rather than as stripes, and survives being 6 px tall in a bar.
const HATCH =
  'repeating-linear-gradient(135deg, oklch(1 0 0 / 0.32) 0 2px, transparent 2px 4px)'

export function VTSwatch({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn('inline-block h-3 w-3 flex-none rounded-[3px] bg-el-vt', className)}
    />
  )
}

export function NTSwatch({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn('inline-block h-3 w-3 flex-none rounded-full bg-el-nt', className)}
      style={{ backgroundImage: HATCH }}
    />
  )
}

/** TariffShareBar is the two-segment share bar. A 2 px surface-coloured gap
 *  separates the segments (the v6 chart rule) so the boundary is a shape and not
 *  a colour transition, and the whole bar carries an aria-label naming both
 *  tariffs with their values — the bar is decoration for a screen reader
 *  otherwise. */
export function TariffShareBar({ vtHaler, ntHaler }: { vtHaler: number; ntHaler: number }) {
  const total = vtHaler + ntHaler
  const vtPct = total > 0 ? (vtHaler / total) * 100 : 50
  const label = `${cs.electricity.word.vtLong} ${Math.round(vtPct)} %, ${cs.electricity.word.ntLong} ${Math.round(100 - vtPct)} %`

  return (
    <div
      role="img"
      aria-label={label}
      className="flex h-3 w-full overflow-hidden rounded-full bg-s3"
    >
      <div className="h-full bg-el-vt" style={{ width: `${vtPct}%` }} />
      <div className="h-full w-[2px] flex-none bg-s1" />
      <div className="h-full flex-1 bg-el-nt" style={{ backgroundImage: HATCH }} />
    </div>
  )
}

/** ApproxMark tags a figure that is INTERPOLATED (D138). It is the visual
 *  vocabulary for "approximate", and the rule that goes with it is absolute:
 *  a Kč figure NEVER receives it. A month's cost is an allocation of exact
 *  interval costs (D159), not a repricing of invented kWh, and the two kinds of
 *  number appear side by side on Historie — the first screen in home where they
 *  do. */
export function ApproxMark() {
  return (
    <span className="ml-1 align-middle text-[10px] font-semibold text-el-approx" title={cs.electricity.history.approxNote}>
      ≈
    </span>
  )
}
