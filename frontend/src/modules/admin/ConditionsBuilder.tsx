// The "poslat jen když" builder (conditions on rules and summaries).
//
// A condition names a catalog KEY (a count, or a list judged by its length),
// an operator and a number — picked, never typed, exactly like the composer's
// tokens. Two or more conditions expose the combinator ("všechny" / "stačí
// jedna"), which is how "reminders today OR overdue items" is said without
// this ever becoming an expression language.

import { Plus, X } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { Button, Input } from '@/components/ui/ui'
import type {
  ConditionOp,
  NotificationCatalog,
  NotificationConditions,
} from '@/api/types'

/** Mirrors the server's maxConditionItems (openapi maxItems: 8) so the limit
 *  is unreachable in the UI instead of a 422 after the round-trip. */
export const MAX_CONDITION_ITEMS = 8

export const CONDITION_OPS: [ConditionOp, string][] = [
  ['gt', cs.admin.opGt],
  ['gte', cs.admin.opGte],
  ['lt', cs.admin.opLt],
  ['lte', cs.admin.opLte],
  ['eq', cs.admin.opEq],
  ['neq', cs.admin.opNeq],
]

/** conditionsValid says whether the block can be saved: every row must have a
 *  key (an empty block is fine — it means "no conditions"). */
export function conditionsValid(c: NotificationConditions | null): boolean {
  return !c || c.items.every((it) => it.key !== '')
}

/** conditionsForSave normalizes for the wire: no rows ⇒ null, which the server
 *  stores as "no conditions". */
export function conditionsForSave(c: NotificationConditions | null): NotificationConditions | null {
  return c && c.items.length > 0 ? c : null
}

/** conditionOptions merges the metric and list catalogs into one picker list —
 *  a key published as both counts the same selection, so it appears once, and
 *  counts as personal when EITHER catalog says so (matching the server's
 *  keyIsPersonal). The trigger context offers exactly the server's trigger
 *  palette — the same household-only set the save validates against, so the
 *  picker can never offer a key a save would refuse — falling back to the
 *  scope flags when the palette is absent. */
export function conditionOptions(
  catalog: NotificationCatalog | undefined,
  context: 'trigger' | 'summary',
): { key: string; label: string }[] {
  const seen = new Map<string, { key: string; label: string; personal: boolean }>()
  for (const m of catalog?.metrics ?? []) {
    seen.set(m.key, { key: m.key, label: m.label, personal: m.scope === 'personal' })
  }
  for (const l of catalog?.lists ?? []) {
    const prev = seen.get(l.key)
    if (prev) prev.personal = prev.personal || l.scope === 'personal'
    else seen.set(l.key, { key: l.key, label: l.label, personal: l.scope === 'personal' })
  }
  const trigger = catalog?.tokens?.trigger
  const allowed =
    trigger &&
    new Set([...(trigger.metric ?? []), ...(trigger.list ?? [])].map((t) => t.replace(/^[^.]+\./, '')))
  return [...seen.values()]
    .filter((o) => context === 'summary' || (allowed ? allowed.has(o.key) : !o.personal))
    .map((o) => ({ key: o.key, label: o.personal ? `${o.label} · osobní` : o.label }))
}

interface ConditionsBuilderProps {
  value: NotificationConditions | null
  onChange: (v: NotificationConditions | null) => void
  catalog?: NotificationCatalog
  context: 'trigger' | 'summary'
  disabled?: boolean
}

export function ConditionsBuilder({ value, onChange, catalog, context, disabled }: ConditionsBuilderProps) {
  const options = conditionOptions(catalog, context)
  const items = value?.items ?? []
  const mode = value?.mode ?? 'all'

  const update = (next: NotificationConditions) => {
    onChange(next.items.length > 0 ? next : null)
  }

  return (
    <div>
      <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.conditions}</span>

      <div className="space-y-2">
        {items.map((it, i) => (
          <div key={i} className="flex flex-wrap items-center gap-1.5">
            <select
              value={it.key}
              disabled={disabled}
              onChange={(e) => {
                const next = items.slice()
                next[i] = { ...it, key: e.target.value }
                update({ mode, items: next })
              }}
              className="h-9 min-w-0 flex-1 rounded-md border border-border bg-s2 px-2 text-[13px] text-fg"
            >
              <option value="">{cs.admin.conditionKeyPlaceholder}</option>
              {options.map((o) => (
                <option key={o.key} value={o.key}>
                  {o.label}
                </option>
              ))}
            </select>
            <select
              value={it.op}
              disabled={disabled}
              onChange={(e) => {
                const next = items.slice()
                next[i] = { ...it, op: e.target.value as ConditionOp }
                update({ mode, items: next })
              }}
              className="h-9 rounded-md border border-border bg-s2 px-2 text-[13px] text-fg"
            >
              {CONDITION_OPS.map(([op, label]) => (
                <option key={op} value={op}>
                  {label}
                </option>
              ))}
            </select>
            <Input
              type="number"
              min={0}
              value={it.value}
              disabled={disabled}
              onChange={(e) => {
                const raw = Math.trunc(Number(e.target.value))
                const next = items.slice()
                next[i] = { ...it, value: Number.isFinite(raw) && raw > 0 ? raw : 0 }
                update({ mode, items: next })
              }}
              className="h-9 w-16 px-2 text-center font-mono text-[13px]"
            />
            <button
              type="button"
              aria-label={cs.admin.conditionRemove}
              disabled={disabled}
              onClick={() => update({ mode, items: items.filter((_, j) => j !== i) })}
              className="grid h-9 w-9 flex-none place-items-center rounded-md border border-border text-muted hover:border-danger hover:text-danger"
            >
              <X size={14} aria-hidden />
            </button>
          </div>
        ))}
      </div>

      {items.length >= 2 && (
        <div className="mt-2 flex flex-wrap gap-1.5" role="radiogroup" aria-label={cs.admin.conditions}>
          {(
            [
              ['all', cs.admin.conditionModeAll],
              ['any', cs.admin.conditionModeAny],
            ] as const
          ).map(([m, label]) => (
            <button
              key={m}
              type="button"
              role="radio"
              aria-checked={mode === m}
              disabled={disabled}
              onClick={() => update({ mode: m, items })}
              className={cn(
                'inline-flex min-h-[32px] items-center rounded-lg border px-2.5 text-[12.5px] font-semibold',
                mode === m ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-s2 text-muted',
                disabled && 'cursor-not-allowed opacity-50',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      )}

      <div className="mt-2">
        <Button
          variant="secondary"
          size="sm"
          disabled={disabled || items.length >= MAX_CONDITION_ITEMS}
          onClick={() => update({ mode, items: [...items, { key: '', op: 'gt', value: 0 }] })}
        >
          <Plus size={14} aria-hidden />
          <span className="ml-1.5">{cs.admin.conditionAdd}</span>
        </Button>
      </div>

      {!conditionsValid(value) && (
        <span className="mt-1 block text-[12px] text-danger">{cs.admin.conditionIncomplete}</span>
      )}
      {items.length > 0 && (
        <p className="mt-1.5 text-[12px] text-muted text-pretty">
          {context === 'trigger' ? cs.admin.conditionsHintRule : cs.admin.conditionsHintSummary}
        </p>
      )}
    </div>
  )
}

/** conditionsPhrase renders a saved block as one Czech line for the list rows,
 *  joined by the combinator. The labels come STRAIGHT off the catalog, not from
 *  conditionOptions: the "· osobní" the picker appends is an affordance for
 *  telling two options apart in a select, and reads as a broken sentence in the
 *  middle of prose. */
export function conditionsPhrase(
  c: NotificationConditions | null,
  catalog?: NotificationCatalog,
): string | null {
  if (!c || c.items.length === 0) return null
  const labels = new Map<string, string>()
  for (const l of catalog?.lists ?? []) labels.set(l.key, l.label)
  // Metrics win a shared key: a list publishes the same Czech label anyway.
  for (const m of catalog?.metrics ?? []) labels.set(m.key, m.label)
  const ops = new Map(CONDITION_OPS)
  const parts = c.items.map((it) => `${labels.get(it.key) ?? it.key} ${ops.get(it.op) ?? it.op} ${it.value}`)
  return parts.join(c.mode === 'any' ? ' nebo ' : ' a ')
}
