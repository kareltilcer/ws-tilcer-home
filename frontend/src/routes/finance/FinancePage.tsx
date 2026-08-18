import { useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { AlertCircle, Plus, Wallet } from 'lucide-react'
import { qk } from '@/api/keys'
import * as api from '@/api/endpoints'
import { ApiError } from '@/api/client'
import type { FinanceMonth, FinanceRates } from '@/api/types'
import { useAuth } from '@/app/auth'
import { useDocumentTitle } from '@/hooks/useDocumentTitle'
import { useOnline, offlineWriteProps } from '@/platform/pwa/offline'
import { cs } from '@/i18n/cs'
import { monthLabel } from '@/i18n/format'
import { count, PLURAL } from '@/i18n/plural'
import { Button, Spinner } from '@/components/ui/ui'
import { cn } from '@/lib/utils'
import { BUCKETS, fmtMoney } from './buckets'
import { Legend } from './AllocationBar'
import { MonthRow } from './MonthRow'
import { MonthFormModal } from './MonthFormModal'
import { DeleteMonthDialog } from './DeleteMonthDialog'

// Finance — the module home (FR-F1). One screen, no tabs: header → stat tiles →
// the missing-month prompt → legend → the month list.
//
// This is a ONCE-A-MONTH destination, which changes the design brief in a
// specific way: nobody builds muscle memory here. Every control is explicitly
// labelled rather than iconographic, and the primary action is impossible to
// miss.
//
// The list arrives FULL — after the `fin` migration it opens with every month
// that service ever held — so the long list, grouped by year with sticky
// headers, is the real screen; the empty state is seen once, ever.

/** currentMonthKey is the local YYYY-MM. It decides the missing-month prompt and
 *  the form's default, and it must agree with the backend's HOME_TIMEZONE view
 *  of "this month" — both are Europe/Prague in practice. */
function currentMonthKey(now = new Date()): string {
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

export function FinancePage() {
  useDocumentTitle(cs.finance.title)
  const { canWrite } = useAuth()
  const online = useOnline()
  const qc = useQueryClient()

  const [openId, setOpenId] = useState<string | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<FinanceMonth | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<FinanceMonth | null>(null)

  // A few dozen rows: no "load more" control — the list IS the cache. But the
  // whole history has to arrive, not just the first page: the tiles below claim
  // ALL-TIME totals and the list is grouped by year, so a truncated page would
  // silently under-report and drop the oldest years with nothing on screen to
  // say so. Follow next_cursor until it runs out — one request today, one more
  // per 200 months of history.
  const q = useQuery({
    queryKey: qk.financeMonths,
    queryFn: async () => {
      const items: FinanceMonth[] = []
      let cursor: string | undefined
      for (;;) {
        const page = await api.listFinanceMonths({ limit: 200, cursor })
        items.push(...page.items)
        // The cursor is the page's oldest month and the next page is strictly
        // older, so this terminates; the empty-page check is belt and braces.
        if (!page.next_cursor || page.items.length === 0) return { items, next_cursor: null }
        cursor = page.next_cursor
      }
    },
  })
  const months = useMemo(() => q.data?.items ?? [], [q.data])

  const writeProps = offlineWriteProps(online)
  const mayWrite = canWrite && !writeProps.disabled
  const writeTitle = writeProps.disabled ? writeProps.title : canWrite ? undefined : cs.common.readOnly

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: qk.financeMonths })
    // The Rozpočet widget's payload rides the dashboard response, so it follows.
    void qc.invalidateQueries({ queryKey: qk.dashboard })
  }

  const save = useMutation({
    mutationFn: (v: { month: string; income_kaja: number; income_andy: number; rates: FinanceRates }) =>
      editing ? api.updateFinanceMonth(editing.id, v) : api.createFinanceMonth(v),
    onSuccess: (m) => {
      toast.success(editing ? cs.finance.updated(monthLabel(m.month)) : cs.finance.created(monthLabel(m.month)))
      setFormOpen(false)
      setFormError(null)
      setOpenId(m.id)
      refresh()
    },
    onError: (err) => {
      // 409 is the one refusal a user can act on — say which month, not "conflict".
      if (err instanceof ApiError && err.status === 409) {
        setFormError(cs.finance.monthExists)
        return
      }
      setFormError(err instanceof ApiError && err.detail ? err.detail : cs.finance.saveError)
    },
  })

  const remove = useMutation({
    mutationFn: (m: FinanceMonth) => api.deleteFinanceMonth(m.id),
    onSuccess: (_v, m) => {
      toast.success(cs.finance.deleted(monthLabel(m.month)))
      setPendingDelete(null)
      setOpenId(null)
      refresh()
    },
    onError: () => toast.error(cs.finance.deleteError),
  })

  const openAdd = () => {
    setEditing(null)
    setFormError(null)
    setFormOpen(true)
  }
  const openEdit = (m: FinanceMonth) => {
    setEditing(m)
    setFormError(null)
    setFormOpen(true)
  }

  // The Rozpočet widget's "Zadat ⟨měsíc⟩" links to the ADD FORM, not merely to
  // this screen, and carries that intent in the navigation state. It is consumed
  // once — replacing the entry — so a later back/forward does not reopen the
  // form on a month the household has since entered.
  const location = useLocation()
  const navigate = useNavigate()
  useEffect(() => {
    if (!(location.state as { add?: boolean } | null)?.add) return
    openAdd()
    navigate(location.pathname, { replace: true, state: null })
  }, [location, navigate])

  const current = currentMonthKey()
  const currentMissing = months.length > 0 && !months.some((m) => m.month === current)

  // The add form's default. The current month is the answer nearly always, but
  // once it is recorded that option is DISABLED in the select — defaulting to it
  // would open the form on a month that cannot be saved and only say so after a
  // 409 came back. So pick the first month the form would actually accept,
  // scanned in the order someone would want it: this month, then backwards
  // through the gaps, and next month only as a last resort. The window matches
  // the one MonthFormModal.monthOptions offers.
  const defaultAddMonth = useMemo(() => {
    if (months.length === 0) return current
    const taken = new Set(months.map((m) => m.month))
    const [y, mo] = current.split('-').map(Number)
    const key = (offset: number) => {
      const d = new Date(y, mo - 1 + offset, 1)
      return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
    }
    const offsets = [...Array.from({ length: 36 }, (_, i) => -i), 1]
    return offsets.map(key).find((ym) => !taken.has(ym)) ?? current
  }, [current, months])

  // Grouped by year, newest first — the treatment the long post-migration list
  // needs. The rows already arrive in month-descending order.
  const years = useMemo(() => {
    const out: { year: string; months: FinanceMonth[] }[] = []
    for (const m of months) {
      const y = m.month.slice(0, 4)
      const last = out[out.length - 1]
      if (last && last.year === y) last.months.push(m)
      else out.push({ year: y, months: [m] })
    }
    return out
  }, [months])

  const tiles = useMemo(() => {
    if (months.length === 0) return []
    const totals = months.reduce(
      (acc, m) => {
        acc.fun += m.split.fun_savings
        acc.nofun += m.split.no_fun_savings
        acc.income += m.split.total_income
        return acc
      },
      { fun: 0, nofun: 0, income: 0 },
    )
    const latest = months[0]
    const monthsLabel = count(months.length, PLURAL.months)
    return [
      {
        key: 'latest',
        label: cs.finance.tileLatest,
        value: fmtMoney(latest.split.total_income),
        meta: cs.finance.tileLatestMeta(monthLabel(latest.month)),
        accent: BUCKETS[0].bg,
      },
      {
        key: 'saved',
        label: cs.finance.tileSaved,
        value: fmtMoney(totals.fun + totals.nofun),
        meta: cs.finance.tileSavedMeta(fmtMoney(totals.fun), fmtMoney(totals.nofun)),
        accent: BUCKETS[2].bg,
      },
      {
        key: 'average',
        label: cs.finance.tileAverage,
        value: fmtMoney(Math.round(totals.income / months.length)),
        meta: cs.finance.tileAverageMeta(monthsLabel),
        accent: BUCKETS[1].bg,
      },
    ]
  }, [months])

  return (
    <div className="mx-auto max-w-5xl">
      <header className="mb-5 flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-extrabold tracking-tight">{cs.finance.title}</h1>
          <p className="mt-1 text-sm text-muted text-pretty">{cs.finance.lede}</p>
        </div>
        {months.length > 0 && (
          <span className="font-mono text-[11.5px] text-subtle">{count(months.length, PLURAL.months)}</span>
        )}
        <Button variant="primary" disabled={!mayWrite} title={writeTitle} onClick={openAdd}>
          <Plus size={16} aria-hidden /> {cs.finance.add}
        </Button>
      </header>

      {q.isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner />
        </div>
      ) : q.isError ? (
        <div className="grid place-items-center py-16 text-center">
          <div className="max-w-sm">
            <div className="mx-auto mb-3.5 grid h-11 w-11 place-items-center rounded-full bg-danger/20 text-danger">
              <AlertCircle size={22} aria-hidden />
            </div>
            <div className="mb-1.5 font-bold">{cs.finance.errorTitle}</div>
            <p className="mb-4 text-sm text-muted text-pretty">{cs.finance.errorBody}</p>
            <Button variant="primary" onClick={() => void q.refetch()}>
              {cs.common.retry}
            </Button>
          </div>
        </div>
      ) : months.length === 0 ? (
        <div className="grid place-items-center py-14">
          <div className="max-w-md rounded-2xl border border-dashed border-border px-6 py-8 text-center">
            <div className="mx-auto mb-3.5 grid h-12 w-12 place-items-center rounded-xl border border-dashed border-border-strong bg-s2 text-subtle">
              <Wallet size={22} aria-hidden />
            </div>
            <div className="mb-1.5 text-lg font-bold">{cs.finance.emptyTitle}</div>
            <p className="mb-4 text-sm text-muted text-pretty">{cs.finance.emptyBody}</p>
            <Button variant="primary" disabled={!mayWrite} title={writeTitle} onClick={openAdd}>
              <Plus size={16} aria-hidden /> {cs.finance.add}
            </Button>
          </div>
        </div>
      ) : (
        <>
          <div className="mb-3.5 grid gap-3.5 sm:grid-cols-3">
            {tiles.map((t) => (
              <div key={t.key} className="relative overflow-hidden rounded-2xl border border-border bg-s1 p-4">
                <span className={cn('absolute inset-y-0 left-0 w-[3px]', t.accent)} aria-hidden />
                <div className="text-[12px] font-semibold text-muted">{t.label}</div>
                <div className="my-1 font-mono text-2xl font-semibold tabular-nums tracking-tight">{t.value}</div>
                <div className="text-[11.5px] text-subtle text-pretty">{t.meta}</div>
              </div>
            ))}
          </div>

          {/* The prompt, not an empty state: a month nobody entered is this
              module's actual failure mode. */}
          {currentMissing && (
            <div className="mb-3.5 flex flex-wrap items-center gap-3 rounded-xl border border-dashed border-accent/55 bg-accent/[0.07] px-3.5 py-3">
              <span className="grid h-[30px] w-[30px] flex-none place-items-center rounded-lg bg-accent-soft text-fg">
                <Wallet size={15} aria-hidden />
              </span>
              <div className="min-w-0 flex-1">
                <div className="text-[13.5px] font-bold">{cs.finance.missingTitle(monthLabel(current))}</div>
                <div className="text-[12px] text-muted">{cs.finance.missingBody}</div>
              </div>
              <Button variant="primary" disabled={!mayWrite} title={writeTitle} onClick={openAdd}>
                {cs.finance.missingAction(monthLabel(current))}
              </Button>
            </div>
          )}

          <Legend className="mb-2.5" />

          <div className="overflow-hidden rounded-2xl border border-border bg-s1">
            <div className="hidden items-center gap-4 border-b border-border bg-s2 px-3.5 py-2.5 font-mono text-[10px] uppercase tracking-[0.06em] text-subtle md:flex">
              <span className="w-[14px] flex-none" />
              <span className="w-[150px] flex-none">{cs.finance.month}</span>
              <span className="flex-1">{cs.finance.allocation}</span>
              <span className="w-[130px] flex-none text-right">{cs.finance.totalIncome}</span>
              <span className="w-[120px] flex-none text-right">{cs.finance.toSavings}</span>
            </div>
            {years.map((g) => (
              <section key={g.year} aria-labelledby={`fin-year-${g.year}`}>
                {/* The count sits OUTSIDE the heading: inside it, the accessible
                    name concatenates into "20268 měsíců". */}
                <div className="sticky top-0 z-[1] flex items-center gap-2.5 border-y border-border bg-s3 px-3.5 py-1.5">
                  <h2 id={`fin-year-${g.year}`} className="font-mono text-[12px] font-bold tracking-[0.04em]">
                    {g.year}
                  </h2>
                  <span className="text-[11.5px] text-subtle">{count(g.months.length, PLURAL.months)}</span>
                </div>
                <ul>
                  {g.months.map((m) => (
                    <MonthRow
                      key={m.id}
                      month={m}
                      open={openId === m.id}
                      onToggle={() => setOpenId((id) => (id === m.id ? null : m.id))}
                      canWrite={mayWrite}
                      writeTitle={writeTitle}
                      onEdit={() => openEdit(m)}
                      onDelete={() => setPendingDelete(m)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        </>
      )}

      {formOpen && (
        <MonthFormModal
          // Remounting per target resets the fields; the modal holds its own
          // draft state and must not inherit the previous month's numbers.
          key={editing?.id ?? 'new'}
          open={formOpen}
          onOpenChange={(o) => {
            setFormOpen(o)
            if (!o) setFormError(null)
          }}
          editing={editing}
          months={months}
          defaultMonth={defaultAddMonth}
          saving={save.isPending}
          error={formError}
          onSubmit={(v) => save.mutate(v)}
        />
      )}

      <DeleteMonthDialog
        month={pendingDelete}
        onOpenChange={(o) => {
          if (!o) setPendingDelete(null)
        }}
        deleting={remove.isPending}
        onConfirm={() => pendingDelete && remove.mutate(pendingDelete)}
      />
    </div>
  )
}
