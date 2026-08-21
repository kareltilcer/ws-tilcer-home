import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleSlash, Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { PLURAL } from '@/i18n/plural'
import { fmtDateISO } from '@/i18n/format'
import { useAuth } from '@/app/auth'
import { useOnline } from '@/platform/pwa/offline'
import { Button } from '@/components/ui/ui'
import { deleteReading, getIntervals, listReadings } from '../api/endpoints'
import type { ElInterval, ElReading } from '../api/types'
import { kc2, kwh, plural } from '../format'
import { ReadingDialog } from '../components/ReadingDialog'
import { NTSwatch, VTSwatch } from '../components/TariffMarks'

// ODEČTY — the list, and the place a mistyped register becomes obvious.
//
// Each row carries THE INTERVAL THAT ENDS AT IT: days, VT/NT kWh, energy Kč and
// which ceník priced it. The rows are built to be COMPARED rather than merely
// read, because that is the whole diagnostic: one interval looking absurd beside
// its neighbours is how a fumbled digit announces itself, and no validation can
// catch a reading that is wrong but still increasing.
//
// ⚠ THE Kč ON A ROW IS ENERGY ONLY, and it is labelled *energie*. Poplatky are
// chunked by (měsíc × ceník version) and belong to no interval (D143);
// allocating them into rows would invent a second rounding rule whose only job
// is keeping two views agreeing. They appear once, on Přehled, as their own line.

export function OdectyTab({
  periodId,
  onAddReading,
}: {
  periodId: string
  onAddReading: (date?: string) => void
}) {
  const online = useOnline()
  const canWrite = useAuth().canWrite && online
  const qc = useQueryClient()
  const [editing, setEditing] = useState<ElReading | null>(null)

  // PAGED, not a single page of 100. This tab is the ONLY place a reading can be
  // corrected or removed, so a reading that falls off the end of the first page
  // becomes uneditable while still feeding every interval and total above it — a
  // mistyped digit from year one, permanently out of reach. The cursor is a DATE
  // (D149), which the server hands back as next_cursor.
  const readings = useInfiniteQuery({
    queryKey: qk.electricityReadings(),
    queryFn: ({ pageParam }) => listReadings(pageParam as string | undefined),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
  const intervals = useQuery({
    queryKey: qk.electricityIntervals(periodId),
    queryFn: () => getIntervals(periodId),
  })

  const remove = useMutation({
    mutationFn: (id: string) => deleteReading(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: qk.electricityAll }),
    onError: () => toast.error(cs.electricity.form.saveFailed),
  })

  if (readings.isPending) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-20 animate-pulse rounded-xl border border-border bg-s1" />
        ))}
      </div>
    )
  }

  const items = readings.data?.pages.flatMap((p) => p.items) ?? []
  // Intervals are keyed by their END date, which is the reading the row is about.
  const byEnd = new Map<string, ElInterval>()
  for (const iv of intervals.data?.items ?? []) byEnd.set(iv.to_on, iv)

  if (items.length === 0) {
    return (
      <EmptyState
        title={cs.electricity.reading.empty}
        hint={cs.electricity.reading.emptyHint}
        canWrite={canWrite}
        onAdd={() => onAddReading(undefined)}
      />
    )
  }

  return (
    <div className="space-y-2 pb-4">
      {items.length === 1 && (
        <p className="rounded-lg border border-border bg-s2 px-3 py-2 text-[12.5px] text-muted text-pretty">
          {cs.electricity.reading.onlyOne}
        </p>
      )}

      {items.map((r) => (
        <ReadingRow
          key={r.id}
          reading={r}
          interval={byEnd.get(r.read_on)}
          canWrite={canWrite}
          onEdit={() => setEditing(r)}
          onDelete={() => {
            if (window.confirm(cs.electricity.reading.deleteConfirm)) remove.mutate(r.id)
          }}
        />
      ))}

      {readings.hasNextPage && (
        <Button
          variant="secondary"
          className="w-full"
          disabled={readings.isFetchingNextPage}
          onClick={() => void readings.fetchNextPage()}
        >
          {readings.isFetchingNextPage
            ? cs.electricity.form.saving
            : cs.electricity.form.loadMore}
        </Button>
      )}

      <ReadingDialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        reading={editing}
      />
    </div>
  )
}

function ReadingRow({
  reading,
  interval,
  canWrite,
  onEdit,
  onDelete,
}: {
  reading: ElReading
  interval?: ElInterval
  canWrite: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  return (
    <article className="rounded-xl border border-border bg-s1 p-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h3 className="text-[14px] font-bold">{fmtDateISO(reading.read_on)}</h3>
        {/* The METER STATE, in the same mono/tabular treatment the form uses, so
            the number on screen looks like the number on the display. */}
        <span className="flex items-center gap-1.5 font-mono text-[13px] tabular-nums">
          <VTSwatch />
          {kwh(reading.vt_dkwh)}
        </span>
        <span className="flex items-center gap-1.5 font-mono text-[13px] tabular-nums">
          <NTSwatch />
          {kwh(reading.nt_dkwh)}
        </span>

        {canWrite && (
          <div className="ml-auto flex gap-1">
            <IconButton label={cs.electricity.form.edit} onClick={onEdit}>
              <Pencil size={14} aria-hidden />
            </IconButton>
            <IconButton label={cs.electricity.form.delete} onClick={onDelete}>
              <Trash2 size={14} aria-hidden />
            </IconButton>
          </div>
        )}
      </div>

      {reading.note && <p className="mt-1 text-[12px] text-subtle text-pretty">{reading.note}</p>}

      {interval && (
        <div className="mt-2 border-t border-border pt-2">
          {interval.blocked ? (
            // The unpriceable row. It is SHOWN rather than hidden, because the
            // straddled price change is exactly what the user needs to see — and
            // it is marked in the informational language, not the destructive one.
            <p className="flex items-center gap-2 text-[12.5px] text-el-blocked">
              <CircleSlash size={14} aria-hidden />
              {cs.electricity.reading.unpriceable}
            </p>
          ) : (
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-[12.5px]">
              <span className="text-muted">
                {cs.electricity.reading.intervalDays} {plural(interval.days, PLURAL.days)}
              </span>
              <span className="flex items-center gap-1.5 font-mono tabular-nums">
                <VTSwatch />+{kwh(interval.vt_dkwh ?? 0)}
              </span>
              <span className="flex items-center gap-1.5 font-mono tabular-nums">
                <NTSwatch />+{kwh(interval.nt_dkwh ?? 0)}
              </span>
              {/* ENERGY ONLY, and it says so. */}
              <span className="font-mono font-semibold tabular-nums">
                {kc2(interval.energy?.total_haler ?? 0)}
                <span className="ml-1 font-sans text-[11px] font-normal text-muted">
                  {cs.electricity.reading.intervalEnergy}
                </span>
              </span>
              {interval.tariff_effective_from && (
                <span className="text-[11.5px] text-subtle">
                  {cs.electricity.reading.pricedBy} {fmtDateISO(interval.tariff_effective_from)}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </article>
  )
}

function IconButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="grid h-9 w-9 place-items-center rounded-md border border-border bg-s2 text-muted hover:text-fg"
    >
      {children}
    </button>
  )
}

export function EmptyState({
  title,
  hint,
  canWrite,
  onAdd,
  addLabel,
}: {
  title: string
  hint: string
  canWrite: boolean
  onAdd: () => void
  addLabel?: string
}) {
  return (
    <div className="grid min-h-[30vh] place-items-center px-2">
      <div className="max-w-sm rounded-2xl border border-dashed border-border p-7 text-center">
        <h2 className="mb-1.5 text-[16px] font-bold">{title}</h2>
        <p className="mb-4 text-[13px] text-muted text-pretty">{hint}</p>
        {/* Disabled and VISIBLE for a reader — never hidden. */}
        <Button onClick={onAdd} disabled={!canWrite}>
          <Plus size={16} aria-hidden />
          {addLabel ?? cs.electricity.word.addReading}
        </Button>
      </div>
    </div>
  )
}
