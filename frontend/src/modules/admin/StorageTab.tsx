import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertTriangle, Info, RefreshCw } from 'lucide-react'
import { qk } from '@/api/keys'
import { getStorageSnapshot } from '@/api/endpoints'
import type { StorageOwnerUsage, StorageSnapshot } from '@/api/types'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { fmtDateTime, fmtMeasuredBytes, fmtNumber, fmtStorageBytes, isMeasuredBytes, UNMEASURED } from '@/i18n/format'
import { Button, Spinner } from '@/components/ui/ui'

/**
 * Administrace → Úložiště (v9, FR-V9-11…13).
 *
 * ⚠ A PAGE OF NUMBERS READ FOUR TIMES A YEAR, by one person, usually because a
 * bill or a warning prompted it. It has no muscle memory to lean on — worse than
 * v8's few-times-a-year case, because the reader is not even the person who filed
 * the data. So: every label written out, every unit stated, no learned
 * iconography, and one line of plain Czech under anything that is not obvious.
 *
 * The ORDER is fixed by what somebody came for: totals → per module → per member →
 * then the two lines that belong to nobody.
 *
 * ⚠ THOSE LAST TWO SIT IN THEIR OWN BLOCK, visually outside the breakdown (D214,
 * D205). They are often LARGER than everything above them and are not part of any
 * per-module sum, so putting them in the same table would make the page read as
 * though its own arithmetic were broken.
 */
export function StorageTab() {
  const qc = useQueryClient()
  const [refreshing, setRefreshing] = useState(false)
  const q = useQuery({
    queryKey: qk.adminStorage,
    queryFn: () => getStorageSnapshot(),
    // The server already caches for 60 s (D195); a second client-side staleTime
    // over the same number is how a page ends up disagreeing with itself.
    staleTime: 0,
  })

  // Obnovit is ONE request, and its answer is the one that lands in the cache.
  //
  // ⚠ Fetching with ?refresh=true and then calling refetch() was two round trips
  // for one number — and the second one read the copy the first had just cached,
  // so it came back `cached: true` and the header stamped freshly-recomputed
  // figures as stale. That inverts what the flag is for. Recomputing is not cheap
  // either: a COUNT(*) per table, a full dbstat scan and a complete bucket listing.
  //
  // ⚠ AND A FAILED REFRESH HAS TO SAY SO. With only a `finally` the rejection went
  // unhandled: the spinner stopped, the previous figures stayed on screen, and the
  // admin read stale numbers as freshly recomputed ones — on the one screen whose
  // entire job is reporting current figures. The cached snapshot is left alone
  // deliberately; losing what WAS measured is worse than keeping it and saying the
  // refresh did not land.
  const refresh = async () => {
    setRefreshing(true)
    try {
      qc.setQueryData(qk.adminStorage, await getStorageSnapshot(true))
    } catch {
      toast.error(cs.storage.refreshError)
    } finally {
      setRefreshing(false)
    }
  }

  if (q.isLoading) {
    return (
      <div className="grid min-h-[220px] place-items-center">
        <Spinner />
      </div>
    )
  }
  if (q.isError || !q.data) {
    return (
      <div className="rounded-2xl border border-danger/50 bg-danger/5 p-6 text-center">
        <div className="mb-1.5 font-bold">{cs.electricity.error.loadFailed}</div>
        <Button className="mt-3" onClick={() => void q.refetch()}>
          {cs.common.retry}
        </Button>
      </div>
    )
  }

  const s = q.data
  return (
    <div className="space-y-4">
      <Totals s={s} onRefresh={refresh} refreshing={refreshing} />
      {s.warning.exceeded && <WarningRegister s={s} />}
      {!s.blobs.available && <BlobsDown reason={s.blobs.error} />}
      {!s.database.bytes_available && <DbstatMissing />}
      <DatabaseBreakdown s={s} />
      <BlobBreakdown s={s} />
      <OutsideBreakdown s={s} />
    </div>
  )
}

// ---- totals ----

function Totals({ s, onRefresh, refreshing }: { s: StorageSnapshot; onRefresh: () => void; refreshing: boolean }) {
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h3 className="text-base font-extrabold tracking-tight">{cs.storage.total}</h3>
        <div className="flex-1" />
        <span className="font-mono text-[11px] text-subtle">
          {cs.storage.generatedAt} {fmtDateTime(s.generated_at)}
          {/* A stale figure must LOOK stale (D195). */}
          {s.cached && ` · ${cs.storage.cached}`}
        </span>
        <Button size="sm" onClick={onRefresh} loading={refreshing}>
          <RefreshCw size={14} aria-hidden /> {cs.storage.refresh}
        </Button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Stat
          label={cs.storage.database}
          value={fmtStorageBytes(s.database.total_bytes)}
          hint={`${cs.storage.wal} ${fmtStorageBytes(s.database.wal_bytes)}${
            s.database.free_bytes !== null ? ` · ${cs.storage.free} ${fmtStorageBytes(s.database.free_bytes)}` : ''
          }`}
        />
        <Stat
          label={cs.storage.objectStorage}
          value={s.blobs.available ? fmtMeasuredBytes(s.blobs.total_bytes) : UNMEASURED}
          unmeasured={!s.blobs.available}
          hint={
            s.blobs.available && s.blobs.total_objects !== null
              ? count(s.blobs.total_objects, PLURAL.objects)
              : undefined
          }
        />
      </div>
    </section>
  )
}

function Stat({
  label,
  value,
  hint,
  unmeasured,
}: {
  label: string
  value: string
  hint?: string
  unmeasured?: boolean
}) {
  return (
    <div className="rounded-xl border border-border bg-s2 p-3">
      <div className="mb-1 text-[12px] text-muted">{label}</div>
      <div className={cn('text-xl font-extrabold tabular-nums', unmeasured && 'font-sans text-base italic text-unmeasured')}>
        {value}
      </div>
      {hint && <div className="mt-1 font-mono text-[11px] text-subtle">{hint}</div>}
    </div>
  )
}

// ---- the warning register ----

/**
 * ⚠ INFORMATIONAL, NOT DESTRUCTIVE (D196). `--attention`, never `--danger`, which
 * means delete — and the copy says outright that nothing is blocked. Nobody has
 * done anything wrong; the bucket is simply larger than a number somebody chose.
 */
function WarningRegister({ s }: { s: StorageSnapshot }) {
  return (
    <section className="rounded-2xl border border-attention bg-attention-soft p-4">
      <div className="flex gap-3">
        <AlertTriangle size={18} className="mt-0.5 flex-none text-attention" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="mb-1 font-bold text-attention">
            {cs.storage.overThreshold} — {cs.storage.thresholdLabel(s.warning.threshold_mb)}
          </div>
          <p className="mb-2 text-[13px] leading-snug text-pretty">{cs.storage.thresholdBody}</p>
          {s.warning.largest_contributors.length > 0 && (
            <>
              <div className="mb-1 text-[12px] font-semibold">{cs.storage.largestContributors}</div>
              <ul className="space-y-0.5">
                {s.warning.largest_contributors.map((c, i) => (
                  <li key={i} className="flex items-center gap-2 font-mono text-[11.5px]">
                    <span className="flex-1 truncate">{ownerLabel(c)}</span>
                    <span className="tabular-nums">{fmtStorageBytes(c.bytes)}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      </div>
    </section>
  )
}

function BlobsDown({ reason }: { reason: string | null }) {
  return (
    <Notice title={cs.storage.blobsDownTitle}>
      {cs.storage.blobsDownBody}
      {reason && <span className="mt-1 block font-mono text-[11px] text-subtle">{reason}</span>}
    </Notice>
  )
}

function DbstatMissing() {
  return <Notice title={cs.storage.dbstatMissingTitle}>{cs.storage.dbstatMissingBody}</Notice>
}

/** Informational, never an error page — `--info`, the "blocked ≠ error" family. */
function Notice({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex gap-3 rounded-2xl border border-info bg-info-soft p-4">
      <Info size={18} className="mt-0.5 flex-none text-info" aria-hidden />
      <div className="min-w-0">
        <div className="mb-0.5 font-bold">{title}</div>
        <p className="text-[13px] leading-snug text-pretty">{children}</p>
      </div>
    </section>
  )
}

// ---- per module ----

function DatabaseBreakdown({ s }: { s: StorageSnapshot }) {
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <h3 className="mb-1 text-base font-extrabold tracking-tight">
        {cs.storage.database} · {cs.storage.byModule}
      </h3>
      <p className="mb-3 text-[12px] text-muted">
        {count(s.database.modules.length, PLURAL.modules)} ·{' '}
        {count(
          s.database.modules.reduce((n, m) => n + m.tables.length, 0),
          PLURAL.tables,
        )}
      </p>
      <div className="space-y-2">
        {s.database.modules.map((m) => (
          <details key={m.module} className="rounded-xl border border-border bg-s2">
            <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-[13px]">
              <span className="flex-1 truncate font-semibold">{m.module}</span>
              <span className="font-mono text-[11px] text-subtle">{count(m.tables.length, PLURAL.tables)}</span>
              <Bytes value={m.bytes} />
            </summary>
            {/* ⚠ THREE COLUMNS, AND THE THIRD IS NOT OPTIONAL. A module's figure
                above is rows + INDEXES (an FTS5 index routinely outweighs what it
                indexes), so printing only `bytes` per table left a column that
                visibly did not add up to the total beside it — on the one page
                whose premise is that the arithmetic works. The reader has no way
                to know the difference is index pages unless the page says so, and
                an unexplained gap reads as a measurement bug.

                The third figure costs width a table name cannot spare at 375 px, so
                the rows scroll sideways below a floor rather than squeezing the one
                column that identifies the row — the same treatment the Soukromé
                položky table uses one tab away. */}
            <div className="overflow-x-auto border-t border-border px-3 py-2">
              <div className="min-w-[360px]">
                <div className="flex items-center gap-2 pb-1 font-mono text-[10px] uppercase tracking-wide text-subtle">
                  <span className="flex-1" />
                  <span className="w-16 text-right">{cs.storage.rows}</span>
                  <span className="w-20 text-right">{cs.storage.size}</span>
                  <span className="w-20 text-right">{cs.storage.indexes}</span>
                </div>
                {m.tables.map((t) => (
                  <div key={t.name} className="flex items-center gap-2 py-1 font-mono text-[11.5px]">
                    <span className="flex-1 truncate text-muted">{t.name}</span>
                    {/* A virtual (FTS5) table owns no pages and is not counted, so it
                        says so instead of showing "0" rows beside a 0 B that reads as
                        an empty index. It used to render *nezměřeno* here, which on
                        this page means "nobody looked" — a signal to act on, raised
                        four times over by tables that were never measurable.
                        Its bytes ARE a measured zero, so leaving the two figures out
                        costs the column nothing: 0 + 0 still sums. */}
                    {t.virtual ? (
                      <span className="truncate font-sans italic text-subtle">{cs.storage.virtualTable}</span>
                    ) : (
                      <>
                        <span className="w-16 text-right tabular-nums text-subtle">{fmtNumber(t.row_count)}</span>
                        <Bytes value={t.bytes} className="w-20 text-right" />
                        <Bytes value={t.index_bytes} className="w-20 text-right" />
                      </>
                    )}
                  </div>
                ))}
                <p className="mt-1.5 border-t border-border pt-1.5 text-[11px] leading-snug text-muted text-pretty">
                  {cs.storage.moduleTotalHint}
                </p>
              </div>
            </div>
          </details>
        ))}
      </div>
    </section>
  )
}

function BlobBreakdown({ s }: { s: StorageSnapshot }) {
  if (!s.blobs.available) return null
  return (
    <section className="rounded-2xl border border-border bg-s1 p-4">
      <h3 className="mb-3 text-base font-extrabold tracking-tight">{cs.storage.objectStorage}</h3>
      <div className="space-y-3">
        {s.blobs.modules.map((m) => (
          <div key={m.module} className="rounded-xl border border-border bg-s2 p-3">
            <div className="mb-2 flex items-center gap-2">
              <span className="flex-1 truncate text-[13px] font-semibold">{m.module}</span>
              <span className="font-mono text-[11px] text-subtle">{m.prefix}</span>
              <Bytes value={m.bytes} />
            </div>
            <div className="space-y-0.5">
              {m.owners.map((o, i) => (
                <div key={i} className="flex items-center gap-2 py-0.5 text-[12px]">
                  <span className="flex-1 truncate">{ownerLabel(o)}</span>
                  <span className="font-mono text-[11px] tabular-nums text-subtle">
                    {count(o.objects, PLURAL.objects)}
                  </span>
                  <span className="w-20 text-right font-mono tabular-nums">{fmtStorageBytes(o.bytes)}</span>
                </div>
              ))}
            </div>
            {/* ⚠ `Nezařazené` is an ORDINARY ROW, not an error — not red, not a
                warning. Without this line the number is meaningless and mildly
                alarming; with it, it is the orphan backlog the mirror job
                reconciles (D194). */}
            {m.owners.some((o) => o.kind === 'unattributed') && (
              <p className="mt-2 border-t border-border pt-2 text-[11.5px] leading-snug text-muted text-pretty">
                {cs.storage.unattributedHint}
              </p>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}

// ---- the two lines that belong to nobody ----

function OutsideBreakdown({ s }: { s: StorageSnapshot }) {
  return (
    <section className="rounded-2xl border border-dashed border-border-strong bg-s1/60 p-4">
      <h3 className="mb-0.5 text-base font-extrabold tracking-tight">{cs.storage.outsideBreakdown}</h3>
      <p className="mb-3 text-[12px] text-muted text-pretty">{cs.storage.outsideBreakdownHint}</p>
      <div className="space-y-2">
        {/* ⚠ The replica is DECLINED, not unimplemented (PRD §V9-12). The copy says
            what is true — the app has no credentials for it — rather than
            implying a feature that is coming. */}
        <OutsideRow
          label={cs.storage.replica}
          value={s.replica.configured ? fmtMeasuredBytes(s.replica.bytes) : cs.storage.replicaOff}
          off={!s.replica.configured}
          hint={s.replica.configured ? undefined : cs.storage.replicaOffHint}
        />
        <OutsideRow
          label={cs.storage.backup}
          value={s.backup.configured ? fmtMeasuredBytes(s.backup.bytes) : cs.storage.backupOff}
          off={!s.backup.configured}
          hint={s.backup.configured && s.backup.bucket ? s.backup.bucket : undefined}
        />
      </div>
    </section>
  )
}

function OutsideRow({ label, value, off, hint }: { label: string; value: string; off: boolean; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-s2 px-3 py-2.5">
      <div className="flex items-center gap-2">
        <span className="flex-1 truncate text-[13px] font-semibold">{label}</span>
        <span className={cn('font-mono text-[12.5px] tabular-nums', off && 'font-sans italic text-unmeasured')}>
          {value}
        </span>
      </div>
      {hint && <p className="mt-1 text-[11.5px] leading-snug text-muted text-pretty">{hint}</p>}
    </div>
  )
}

// ---- shared bits ----

/**
 * Bytes renders a nullable figure.
 *
 * ⚠ THE RULE, STATED ONCE: a null renders as *nezměřeno*, NEVER as `0 B` (D193).
 * And it renders in a DIFFERENT TYPE FAMILY — proportional italic where a mono
 * numeral would sit — so the absence is recognisable before it is read rather than
 * after. A zero nobody measured is a lie that looks like good news.
 */
function Bytes({ value, className }: { value: number | null; className?: string }) {
  const missing = !isMeasuredBytes(value)
  return (
    <span
      className={cn(
        'font-mono text-[12px] tabular-nums',
        // ⚠ `italic` ALONE. Pairing it with `not-italic` is not belt-and-braces:
        // both set font-style, the winner is decided by stylesheet order rather
        // than by the order written here, and Tailwind emits `.not-italic` last —
        // so the different-type-family treatment silently never rendered.
        missing && 'font-sans italic text-unmeasured',
        className,
      )}
    >
      {fmtMeasuredBytes(value)}
    </span>
  )
}

function ownerLabel(o: StorageOwnerUsage): string {
  let base: string
  if (o.kind === 'shared') base = cs.storage.kindShared
  else if (o.kind === 'unattributed') base = cs.storage.kindUnattributed
  else base = `${cs.storage.kindPrivate} — ${o.owner_label ?? o.owner_user_id ?? '?'}`
  // Contributor rows are flattened across modules; the module name is the only
  // thing telling two same-kind rows apart. Per-module `owners` rows carry none.
  return o.module ? `${o.module} · ${base}` : base
}
