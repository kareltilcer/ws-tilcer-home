import { useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { CheckCircle2, History, Undo2 } from 'lucide-react'
import { qk } from '@/api/keys'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Input } from '@/components/ui/ui'
import { ResponsiveModal } from '@/components/ui/modal'
import { cn } from '@/lib/utils'
import { dismissWarning, restoreWarning } from '../api/endpoints'
import type { GardenCheckResult, GardenWarning } from '../api/types'
import { SEVERITY_RANK, SeverityChip } from './labels'

// KONTROLA PLÁNU — eleven checks that must not become wallpaper.
//
// This is the hardest UX problem in the module, harder than any layout in it.
// The check returns up to eleven kinds of finding at four severities, and a panel
// of yellow triangles is read twice and ignored forever. Four decisions carry it:
//
//  1. SEVERITY READS WITHOUT COLOUR — icon and word, see SeverityChip.
//  2. GARDEN-WIDE FINDINGS ARE MARKED. C3 rotation, C5 workload, C6
//     concentration and C8 succession are invisible from inside a single bed, so
//     the panel says which findings are about the garden rather than about a
//     bed. Without that, the inline bed badges and this panel appear to
//     disagree.
//  3. DISMISSAL IS A FIRST-CLASS ACTION with a note, and a dismissed warning
//     stays FINDABLE. Without dismissal you stop reading the panel by April, and
//     the panel is the feature.
//  4. A TIP IS NOT A WARNING. C8's legume tip is praise, and it renders as such.

/** Checks whose findings are about the whole garden rather than one bed. */
const GARDEN_WIDE = new Set(['C3', 'C5', 'C6', 'C8'])

export function CheckPanel({
  year,
  check,
  canWrite,
  onFocusBed,
}: {
  year: number
  check: GardenCheckResult | undefined
  canWrite: boolean
  onFocusBed?: (bedId: string) => void
}) {
  const qc = useQueryClient()
  const [showDismissed, setShowDismissed] = useState(false)
  const [dismissing, setDismissing] = useState<GardenWarning | null>(null)
  const [note, setNote] = useState('')

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: qk.gardenAll })
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const dismiss = useMutation({
    mutationFn: ({ key, note }: { key: string; note: string }) =>
      dismissWarning(year, key, note.trim() || undefined),
    onSuccess: () => {
      setDismissing(null)
      setNote('')
      invalidate()
    },
    onError: () => toast.error(cs.common.errorTitle),
  })

  const restore = useMutation({
    mutationFn: (key: string) => restoreWarning(year, key),
    onSuccess: invalidate,
    onError: () => toast.error(cs.common.errorTitle),
  })

  const { active, dismissed } = useMemo(() => {
    const all = [...(check?.warnings ?? [])].sort(
      (a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] || a.check.localeCompare(b.check),
    )
    return {
      active: all.filter((w) => !w.dismissed),
      dismissed: all.filter((w) => w.dismissed),
    }
  }, [check])

  const noHistory = check?.history.status === 'no_history'

  // THE SAME NUMBER THE MODULE BADGE SHOWS: errors and warnings only, which is
  // what GardenPage's header and the Přehled tile count. `active` still carries
  // the info and tip rows and they still render below — but counting them here
  // made the panel read "5 varování" under a badge reading 0, which looks like a
  // stale count rather than like two different questions.
  const openCount = (check?.counts.error ?? 0) + (check?.counts.warn ?? 0)

  return (
    <section id="kontrola" className="rounded-lg border border-border bg-s1">
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <h2 className="flex-1 text-base font-bold">{cs.garden.kontrolaPlanu}</h2>
        {openCount > 0 && (
          <span className="font-mono text-[12px] text-muted">{count(openCount, PLURAL.warnings)}</span>
        )}
        {dismissed.length > 0 && (
          <button
            type="button"
            onClick={() => setShowDismissed((v) => !v)}
            className="text-[12px] font-semibold text-accent underline-offset-2 hover:underline"
          >
            {showDismissed ? cs.garden.varovani : `${cs.garden.showDismissed} (${dismissed.length})`}
          </button>
        )}
      </header>

      <div className="space-y-2.5 p-4">
        {/* THE CHECK THAT CANNOT RUN (D120). It is rendered BEFORE the findings
            and never as a passing tick: rotation is the flagship warning, and in
            year one it does nothing — so it says so, matter-of-factly. */}
        {noHistory && (
          <div className="flex items-start gap-2.5 rounded-md border border-border bg-s2 px-3 py-2.5">
            <History size={16} className="mt-0.5 flex-none text-subtle" aria-hidden />
            <div>
              <div className="text-[13px] font-semibold">{cs.garden.noHistoryTitle}</div>
              <p className="mt-0.5 text-[12.5px] text-muted text-pretty">{cs.garden.noHistoryBody}</p>
            </div>
          </div>
        )}

        {active.length === 0 && !showDismissed && (
          <div className="flex items-start gap-2.5 rounded-md border border-good/40 bg-good/5 px-3 py-2.5">
            <CheckCircle2 size={16} className="mt-0.5 flex-none text-good" aria-hidden />
            <div>
              <div className="text-[13px] font-semibold">{cs.garden.checkClean}</div>
              <p className="mt-0.5 text-[12.5px] text-muted text-pretty">{cs.garden.checkCleanBody}</p>
            </div>
          </div>
        )}

        {(showDismissed ? dismissed : active).map((w) => (
          <WarningRow
            key={w.key}
            warning={w}
            canWrite={canWrite}
            onDismiss={() => {
              setDismissing(w)
              setNote('')
            }}
            onRestore={() => restore.mutate(w.key)}
            onFocusBed={onFocusBed}
          />
        ))}
      </div>

      <ResponsiveModal
        open={Boolean(dismissing)}
        onOpenChange={(open) => !open && setDismissing(null)}
        title={cs.garden.dismissTitle}
      >
        <div className="space-y-3">
          <p className="text-[13px] text-muted text-pretty">{cs.garden.dismissBody}</p>
          {dismissing && (
            <div className="rounded-md border border-border bg-s2 px-3 py-2 text-[13px] font-semibold">
              {dismissing.title_cs}
            </div>
          )}
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-semibold">{cs.garden.dismissNote}</span>
            <Input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder={cs.garden.dismissNotePlaceholder}
              autoFocus
            />
          </label>
          <div className="flex justify-end gap-2 pt-1">
            <Button variant="secondary" onClick={() => setDismissing(null)}>
              {cs.finance.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={!dismissing || dismiss.isPending}
              onClick={() => dismissing && dismiss.mutate({ key: dismissing.key, note })}
            >
              {cs.garden.dismissConfirm}
            </Button>
          </div>
        </div>
      </ResponsiveModal>
    </section>
  )
}

function WarningRow({
  warning,
  canWrite,
  onDismiss,
  onRestore,
  onFocusBed,
}: {
  warning: GardenWarning
  canWrite: boolean
  onDismiss: () => void
  onRestore: () => void
  onFocusBed?: (bedId: string) => void
}) {
  const gardenWide = GARDEN_WIDE.has(warning.check)
  return (
    <article
      className={cn(
        'rounded-md border px-3 py-2.5',
        warning.dismissed ? 'border-dashed border-border bg-transparent opacity-70' : 'border-border bg-s2',
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <SeverityChip severity={warning.severity} />
        {gardenWide && (
          <span className="rounded-md border border-border bg-s3 px-1.5 py-0.5 text-[10.5px] font-semibold text-muted">
            {cs.garden.checkGardenWide}
          </span>
        )}
        <span className="min-w-0 flex-1 text-[13.5px] font-semibold text-pretty">{warning.title_cs}</span>
      </div>

      {warning.detail_cs && (
        <p className="mt-1.5 text-[12.5px] text-muted text-pretty">{warning.detail_cs}</p>
      )}

      {warning.dismissed && warning.dismissed_note && (
        <p className="mt-1.5 text-[12px] text-subtle italic">„{warning.dismissed_note}“</p>
      )}

      <div className="mt-2 flex flex-wrap items-center gap-2">
        {warning.bed_id && onFocusBed && (
          <button
            type="button"
            onClick={() => onFocusBed(warning.bed_id!)}
            className="text-[12px] font-semibold text-accent underline-offset-2 hover:underline"
          >
            {cs.garden.zahon}
          </button>
        )}
        <div className="flex-1" />
        {warning.dismissed ? (
          <Button size="sm" variant="ghost" onClick={onRestore} disabled={!canWrite}>
            <Undo2 size={14} aria-hidden />
            {cs.garden.restore}
          </Button>
        ) : (
          <Button
            size="sm"
            variant="ghost"
            onClick={onDismiss}
            disabled={!canWrite}
            title={canWrite ? undefined : cs.common.readOnly}
          >
            {cs.garden.ignorovat}
          </Button>
        )}
      </div>
    </article>
  )
}

/** BedWarningBadge is the INLINE surface — what is wrong *here*. It and the
 *  panel must never disagree, so both read the same check result and this one
 *  simply filters it by bed. */
export function BedWarningBadge({ warnings }: { warnings: GardenWarning[] }) {
  const open = warnings.filter((w) => !w.dismissed)
  if (open.length === 0) return null
  const worst = open.reduce((a, b) => (SEVERITY_RANK[a.severity] <= SEVERITY_RANK[b.severity] ? a : b))
  return (
    <span title={open.map((w) => w.title_cs).join('\n')}>
      <SeverityChip severity={worst.severity} className="text-[10.5px]" />
    </span>
  )
}
