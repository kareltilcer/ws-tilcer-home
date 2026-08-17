import type { FinanceSplit } from '@/api/types'
import { cn } from '@/lib/utils'
import { BUCKETS, bucketValues, fmtMoney, type Bucket } from './buckets'

/** The gap between bar segments, in px. Declared once because the segment widths
 *  have to subtract their share of it — see AllocationBar. */
const GAP = 2

/** BucketSwatch is the shared colour+shape chip. Never used without its label or
 *  mark beside it. */
export function BucketSwatch({ bucket, size = 11 }: { bucket: Bucket; size?: number }) {
  return <span aria-hidden className={cn('flex-none', bucket.bg, bucket.shape)} style={{ width: size, height: size }} />
}

/** Legend is ALWAYS present wherever the bar is — it is part of what makes the
 *  four buckets separable, not an optional nicety. */
export function Legend({ className }: { className?: string }) {
  return (
    <ul className={cn('flex flex-wrap items-center gap-x-4 gap-y-1.5', className)}>
      {BUCKETS.map((b) => (
        <li key={b.key} className="inline-flex items-center gap-2 text-[12.5px] text-muted">
          <BucketSwatch bucket={b} />
          <span className="font-mono text-[10px] font-bold text-subtle">{b.mark}</span>
          {b.label}
        </li>
      ))}
    </ul>
  )
}

/**
 * AllocationBar is the one place all four buckets sit edge to edge, so it is the
 * palette's hardest test and carries every separator the design specifies:
 *
 *  - a 2 px gap in the SURFACE colour between segments — the gap does the
 *    separating work colour cannot;
 *  - 4 px rounded ends on the bar's outer ends only;
 *  - a minimum 3 % width so a tiny bucket stays visible and hoverable;
 *  - the bucket's mark inside any segment wide enough to hold it;
 *  - a title carrying the Czech label and the value, for hover — and an aria
 *    label naming every bucket, so the bar is never colour alone.
 */
export function AllocationBar({
  // 9 %, not the round 10: this household's standing rates put both savings
  // buckets at exactly 10 % of the total, and a threshold of 10 or 11 would hide
  // the Z and N marks on every single row.
  markMinPercent = 9,
  split,
  height = 16,
  className,
  decorative = false,
}: {
  split: FinanceSplit
  markMinPercent?: number
  height?: number
  className?: string
  /** Drop the bar out of the a11y tree. For a bar sitting INSIDE a control:
   *  name-from-content traversal picks up a descendant's aria-label, so the
   *  label below would otherwise be concatenated into that control's accessible
   *  name — four bucket labels and four amounts, on every row. The numbers are
   *  spelled out in the panel the control expands, so nothing is lost. */
  decorative?: boolean
}) {
  const values = bucketValues(split).filter((b) => b.value > 0)
  const total = values.reduce((n, b) => n + b.value, 0)
  if (total <= 0) {
    return <span className={cn('block rounded-[4px] bg-s3', className)} style={{ height }} aria-hidden />
  }

  // Give every non-zero bucket at least 3 %, then renormalise so the widths still
  // add up to 100 %.
  const raw = values.map((b) => Math.max(3, (b.value / total) * 100))
  const scale = 100 / raw.reduce((a, b) => a + b, 0)
  // The percentages already total 100 %, so the gaps between segments are EXTRA
  // width. Nothing absorbs them — the segments do not shrink — so without this
  // the bar overruns its box by 2 px × (n − 1) and overflow-hidden clips the last
  // segment's rounded end off. Each segment gives back its share of the gaps.
  const gapShare = (GAP * (values.length - 1)) / values.length

  return (
    <span
      className={cn('flex overflow-hidden rounded-[4px] bg-s1', className)}
      style={{ height, gap: GAP }}
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : values.map((b) => `${b.label}: ${fmtMoney(b.value)}`).join(', ')}
      aria-hidden={decorative || undefined}
    >
      {values.map((b, i) => {
        const width = raw[i] * scale
        return (
          <span
            key={b.key}
            title={`${b.label} · ${fmtMoney(b.value)}`}
            className={cn(
              'grid place-items-center overflow-hidden',
              b.bg,
              i === 0 && 'rounded-l-[4px]',
              i === values.length - 1 && 'rounded-r-[4px]',
            )}
            style={{ flex: `0 0 calc(${width.toFixed(2)}% - ${gapShare.toFixed(2)}px)` }}
          >
            {width >= markMinPercent && (
              <span className="font-mono text-[9px] font-bold text-bg" aria-hidden>
                {b.mark}
              </span>
            )}
          </span>
        )
      })}
    </span>
  )
}
