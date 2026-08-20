import { AlertTriangle, CircleAlert, Info, Sparkles, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import type { GardenSeverity, GardenWindow } from '../api/types'

// SEVERITY AND FAMILY MUST READ WITHOUT COLOUR.
//
// This module is used outdoors, on a phone, in sunlight, at 40 % brightness. A
// hue difference that survives the studio does not survive June — so every
// meaning here is carried by an ICON AND A WORD, with colour as reinforcement
// rather than as the message.
//
// The family case is stronger still: the enum has FIFTEEN families and the
// palette has five categorical slots. Fifteen hues cannot pass a CVD all-pairs
// check at any chroma, and a fifteen-item colour legend is unreadable regardless
// of vision. So family is a chip with a LABEL — never a colour alone, and never
// a colour that means something on its own.

const SEVERITY_ICON: Record<GardenSeverity, LucideIcon> = {
  error: CircleAlert,
  warn: AlertTriangle,
  info: Info,
  // A tip is PRAISE, not a warning. If it renders in the same visual language as
  // an error, the panel teaches people that everything in it is noise.
  tip: Sparkles,
}

const SEVERITY_CLASS: Record<GardenSeverity, string> = {
  error: 'border-danger/45 bg-danger/10 text-danger',
  warn: 'border-warn/45 bg-warn/10 text-warn',
  info: 'border-border bg-s2 text-muted',
  tip: 'border-good/45 bg-good/10 text-good',
}

/** SeverityChip renders a severity as icon + word. Both, always. */
export function SeverityChip({ severity, className }: { severity: GardenSeverity; className?: string }) {
  const Icon = SEVERITY_ICON[severity] ?? Info
  return (
    <span
      className={cn(
        'inline-flex flex-none items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-bold',
        SEVERITY_CLASS[severity] ?? SEVERITY_CLASS.info,
        className,
      )}
    >
      <Icon size={13} aria-hidden />
      {cs.garden.severity[severity] ?? severity}
    </span>
  )
}

/** severityRank orders a panel: errors first, tips last. */
export const SEVERITY_RANK: Record<GardenSeverity, number> = { error: 0, warn: 1, info: 2, tip: 3 }

/** FamilyChip is a LABELLED chip. Never colour alone (design §v7 tokens). */
export function FamilyChip({ label, className }: { label: string; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex flex-none items-center rounded-md border border-border bg-s2 px-1.5 py-0.5 text-[10.5px] font-semibold text-muted',
        className,
      )}
      title={cs.garden.celed + ': ' + label}
    >
      {label}
    </span>
  )
}

/** UnverifiedBadge marks machine-written content (D114).
 *
 *  The tone is the design problem, not the pixels: it must read as BOOKKEEPING,
 *  not as a claim that the data is wrong. Most of it will be fine, and a scary
 *  badge on forty crops teaches people to ignore it. */
export function UnverifiedBadge({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex flex-none items-center rounded-md border border-border bg-s3 px-1.5 py-0.5 text-[10.5px] font-semibold text-subtle',
        className,
      )}
      title={cs.garden.unverifiedHint}
    >
      {cs.garden.neovereno}
    </span>
  )
}

// ---- dates and windows ----

const MONTHS_SHORT = ['', '1.', '2.', '3.', '4.', '5.', '6.', '7.', '8.', '9.', '10.', '11.', '12.']

/** fmtDay renders an ISO date as `d. M.` — the compact form used in lists. */
export function fmtDay(iso: string): string {
  const [, m, d] = iso.split('-')
  if (!m || !d) return iso
  return `${Number(d)}. ${MONTHS_SHORT[Number(m)] ?? m}`
}

/** fmtDayYear renders `d. M. yyyy` (PRD §5 conventions). */
export function fmtDayYear(iso: string): string {
  const [y, m, d] = iso.split('-')
  if (!y || !m || !d) return iso
  return `${Number(d)}. ${Number(m)}. ${y}`
}

/** fmtWindow writes a window as a RANGE — "15.–25. 5." — never as a single
 *  date. "Vysaď rajčata mezi 15. a 25. května" is the truth, and a calendar that
 *  draws it as a point is lying about how gardening works. */
export function fmtWindow(from: string, to: string): string {
  if (from === to) return fmtDayYear(from)
  const [fy, fm, fd] = from.split('-')
  const [ty, tm, td] = to.split('-')
  if (fy === ty && fm === tm) return `${Number(fd)}.–${Number(td)}. ${Number(tm)}. ${ty}`
  if (fy === ty) return `${Number(fd)}. ${Number(fm)}. – ${Number(td)}. ${Number(tm)}. ${ty}`
  return `${fmtDayYear(from)} – ${fmtDayYear(to)}`
}

/** describeWindow renders a knowledge-base window in words, for the crop form's
 *  summary line: "týden 10–13" or "42–56 dní před posledním mrazem". */
export function describeWindow(w: GardenWindow | null | undefined): string {
  if (!w) return '—'
  if (w.anchor === 'week') {
    return w.from === w.to ? `týden ${w.from}` : `týden ${w.from}–${w.to}`
  }
  const anchor = w.anchor === 'last_frost' ? 'posledním jarním mrazem' : 'prvním podzimním mrazem'
  const lo = Math.min(w.from, w.to)
  const hi = Math.max(w.from, w.to)
  if (hi <= 0) return `${Math.abs(hi)}–${Math.abs(lo)} dní před ${anchor}`
  if (lo >= 0) return `${lo}–${hi} dní po ${anchor}`
  return `${Math.abs(lo)} dní před až ${hi} dní po ${anchor}`
}
