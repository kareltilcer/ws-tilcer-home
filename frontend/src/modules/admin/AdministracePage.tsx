// Administrace (PRD §V5-7, HANDOFF-design §v5 §1/§3/§4/§5/§7).
//
// Admin-only, four tabs, no reader state — for a non-admin the module does not
// exist (the nav omits it and the route refuses it). The one notification
// surface everyone DOES get lives in Nastavení → Oznámení instead.

import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Send, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count, PLURAL } from '@/i18n/plural'
import { qk } from '@/api/keys'
import { createNotificationRule, createNotificationSchedule, deleteNotificationRule, deleteNotificationSchedule, getNotificationCatalog, listDeliveries, listNotificationRules, listNotificationSchedules, sendBroadcast, testNotificationRule, testNotificationSchedule, updateNotificationRule, updateNotificationSchedule } from './api/endpoints'
import type { ActionDescriptor, Audience, DeliveryStatus, NotificationCatalog, NotificationRule, NotificationSchedule, SendResult } from './api/types'
import { ApiError, apiErrorMessage } from '@/api/client'
import { Button, Input } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { useOnline } from '@/platform/pwa/offline'
import { Composer } from './Composer'
import { AudiencePicker, audienceValid } from './AudiencePicker'
import { ScheduleBuilder } from './ScheduleBuilder'
import { ConditionsBuilder, conditionsForSave, conditionsPhrase, conditionsValid } from './ConditionsBuilder'
import { StorageTab } from './StorageTab'
import { PrivateItemsTab } from './PrivateItemsTab'
import { LimitsTab } from './LimitsTab'

type Tab = 'send' | 'rules' | 'summaries' | 'deliveries' | 'storage' | 'private' | 'limits'

/**
 * Administrace's navigation, at six tabs (v9).
 *
 * ⚠ TWO LEVELS, NOT ONE FLAT ROW. Six pills overflow horizontally at 375 px, and
 * the six are not peers anyway: four configure NOTIFICATIONS and two are storage
 * MAINTENANCE. Grouping them says which is which before anything is read.
 *
 * PRD §V9-7 asked for a single six-tab strip; the delivered design (2026-08-23,
 * later document) uses these two levels, and that is what is built. Level 1 reuses
 * v7's module tab-strip pattern rather than inventing a second pattern for the
 * same job (D202) — v9 adds no nav entry, no widget, and no route to the shell.
 */
const TAB_GROUPS = [
  { key: 'notif', label: 'Notifikace', tabs: ['send', 'rules', 'summaries', 'deliveries'] },
  // v10 (D263): a third sub-tab, because the two chat thresholds are now EDITABLE
  // rather than environment variables — and an operator setting that has to be
  // changed in Coolify is not editable in Administrace, which is what D236 answers.
  { key: 'store', label: 'Správa úložiště', tabs: ['storage', 'private', 'limits'] },
] as const satisfies readonly { key: string; label: string; tabs: readonly Tab[] }[]

/** Mirrors maxCoalesceWindowSeconds in admin/service.go — the editor must not be
 *  able to compose a rule the server will refuse. */
const MAX_COALESCE_SECONDS = 24 * 60 * 60

function clampCoalesce(n: number): number {
  if (!Number.isFinite(n)) return 0
  return Math.min(MAX_COALESCE_SECONDS, Math.max(0, Math.trunc(n)))
}

const TAB_LABELS: Record<Tab, string> = {
  send: cs.admin.tabSend,
  rules: cs.admin.tabRules,
  summaries: cs.admin.tabSummaries,
  deliveries: cs.admin.tabDeliveries,
  storage: cs.storage.title,
  private: cs.privateItems.title,
  limits: cs.storage.limitsTab,
}

export function AdministracePage() {
  const [tab, setTab] = useState<Tab>('send')
  const catalogQuery = useQuery({ queryKey: qk.adminCatalog, queryFn: getNotificationCatalog })
  const catalog = catalogQuery.data
  const currentGroup = TAB_GROUPS.find((g) => (g.tabs as readonly Tab[]).includes(tab)) ?? TAB_GROUPS[0]

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <ScreenHeader title={cs.admin.title} subtitle={cs.admin.subtitle} />

      {/* Level 1 — the group. v7's tab-strip pattern: underline, scroll-snap, no
          wrap, so it degrades to a horizontal scroll at 375 px instead of
          reflowing into two ragged rows. */}
      <div
        role="tablist"
        aria-label={cs.admin.title}
        className="om-scroll flex gap-1 overflow-x-auto border-b border-border"
        style={{ scrollSnapType: 'x proximity' }}
      >
        {TAB_GROUPS.map((g) => {
          const active = (g.tabs as readonly Tab[]).includes(tab)
          return (
            <button
              key={g.key}
              type="button"
              role="tab"
              aria-selected={active}
              // Selecting a group lands on its FIRST tab rather than remembering
              // a per-group position: one less piece of state, and the first tab
              // of each group is the one somebody opening it wants.
              onClick={() => setTab(g.tabs[0])}
              className={cn(
                'min-h-[38px] flex-none scroll-ms-1 whitespace-nowrap border-b-2 px-3.5 text-[13.5px]',
                active ? 'border-accent font-bold text-fg' : 'border-transparent font-semibold text-muted',
              )}
              style={{ scrollSnapAlign: 'start' }}
            >
              {g.label}
            </button>
          )
        })}
      </div>

      {/* Level 2 — the tab within the group. Deliberately lighter than level 1 so
          the two rows do not read as one confused strip. */}
      <div
        role="tablist"
        aria-label={currentGroup.label}
        className="om-scroll -mt-2 flex gap-1 overflow-x-auto"
        style={{ scrollSnapType: 'x proximity' }}
      >
        {(currentGroup.tabs as readonly Tab[]).map((key) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            onClick={() => setTab(key)}
            className={cn(
              'min-h-[34px] flex-none scroll-ms-1 whitespace-nowrap rounded-lg border px-3 text-[12.5px] font-semibold',
              tab === key ? 'border-accent bg-accent-soft text-accent' : 'border-transparent text-muted',
            )}
            style={{ scrollSnapAlign: 'start' }}
          >
            {TAB_LABELS[key]}
          </button>
        ))}
      </div>

      {tab === 'send' && <BroadcastTab catalog={catalog} />}
      {tab === 'rules' && <RulesTab catalog={catalog} />}
      {tab === 'summaries' && <SummariesTab catalog={catalog} />}
      {tab === 'deliveries' && <DeliveriesTab />}
      {tab === 'storage' && <StorageTab />}
      {tab === 'private' && <PrivateItemsTab />}
      {tab === 'limits' && <LimitsTab />}
    </div>
  )
}

// ---- Rozeslat ----

function BroadcastTab({ catalog }: { catalog?: NotificationCatalog }) {
  const online = useOnline()
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [audience, setAudience] = useState<Audience>({ scope: 'all' })
  const [result, setResult] = useState<SendResult | null>(null)

  const send = useMutation({
    mutationFn: () => sendBroadcast({ title, body, audience }),
    onSuccess: (res) => {
      setResult(res)
      toast.success(
        `${cs.admin.sentTitle} — ${count(res.recipients, PLURAL.people)} · ${count(res.subscriptions, PLURAL.devices)}`,
      )
    },
    onError: (e) => toast.error(errMessage(e)),
  })

  // The audience is part of "is this sendable", not an afterthought: an empty
  // role/user selection is a 422 the picker has already flagged in red.
  const canSend = title.trim() !== '' && body.trim() !== '' && audienceValid(audience) && online

  return (
    <section className="space-y-4 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <div>
        <h2 className="text-base font-extrabold">{cs.admin.sendHeading}</h2>
        <p className="text-[12.5px] text-muted">{cs.admin.sendHint}</p>
      </div>

      <Composer
        context="broadcast"
        catalog={catalog}
        title={title}
        body={body}
        onTitleChange={setTitle}
        onBodyChange={setBody}
        disabled={!online}
      />

      <AudiencePicker value={audience} onChange={setAudience} members={catalog?.members ?? []} disabled={!online} />

      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="primary"
          disabled={!canSend}
          loading={send.isPending}
          title={online ? undefined : cs.offline.writeBlocked}
          onClick={() => send.mutate()}
        >
          <Send size={16} aria-hidden />
          <span className="ml-2">{cs.admin.send}</span>
        </Button>
        {result && (
          <span className="text-[12.5px] text-muted" role="status">
            {cs.admin.sentTitle}: {count(result.recipients, PLURAL.people)} ·{' '}
            {count(result.subscriptions, PLURAL.devices)}
          </span>
        )}
      </div>
    </section>
  )
}

// ---- Pravidla ----

function RulesTab({ catalog }: { catalog?: NotificationCatalog }) {
  const online = useOnline()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Partial<NotificationRule> | null>(null)

  const rules = useQuery({ queryKey: qk.adminRules, queryFn: () => listNotificationRules({ limit: 100 }) })

  const save = useMutation({
    mutationFn: (draft: Partial<NotificationRule>) => {
      // The window is a matched pair — null both unless both are set, so a
      // half-filled pair can never reach the server (the editor disables save
      // for that state; this is the backstop).
      const hasWindow = !!(draft.active_from_local && draft.active_to_local)
      const body: Record<string, unknown> = {
        name: draft.name ?? '',
        audience: draft.audience ?? { scope: 'all' },
        title_template: draft.title_template || null,
        body_template: draft.body_template || null,
        coalesce_window_seconds: draft.coalesce_window_seconds ?? 60,
        exclude_actor: draft.exclude_actor ?? false,
        enabled: draft.enabled ?? true,
        // The full block travels every save; no rows ⇒ null, which clears a
        // previously stored one.
        conditions: conditionsForSave(draft.conditions ?? null),
        active_from_local: hasWindow ? draft.active_from_local : null,
        active_to_local: hasWindow ? draft.active_to_local : null,
        // An action key is a BARE VERB and bare verbs are not unique across
        // modules, so the module the admin picked from has to travel with it —
        // the server matches on action alone unless filter_module says otherwise.
        filter_module: draft.filter_module || null,
      }
      // `null` genuinely CLEARS a field on the server, so the trigger pair is
      // sent as a matched set and only when this editor actually owns one:
      // blanket nulls would wipe the action_prefix of a rule this UI cannot
      // even display (it offers exact keys only), leaving an unsavable rule.
      //
      // Which means sending NEITHER is a silent no-op — the server would keep the
      // stored trigger and answer 200 for a change that never happened. That is
      // refused before it reaches the wire; hasTrigger below disables the button
      // for the same state, so this is the backstop rather than the message the
      // admin normally sees.
      if (draft.action_key) {
        body.action_key = draft.action_key
        body.action_prefix = null
      } else if (draft.action_prefix) {
        body.action_prefix = draft.action_prefix
        body.action_key = null
      } else {
        return Promise.reject(new ApiError(422, 'unprocessable', cs.admin.ruleTriggerRequired))
      }
      return draft.id ? updateNotificationRule(draft.id, body) : createNotificationRule(body)
    },
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: qk.adminRules })
    },
    onError: (e) => toast.error(errMessage(e)),
  })

  const toggle = useMutation({
    mutationFn: (r: NotificationRule) => updateNotificationRule(r.id, { enabled: !r.enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: qk.adminRules }),
    onError: (e) => toast.error(errMessage(e)),
  })

  const remove = useMutation({
    mutationFn: (id: string) => deleteNotificationRule(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: qk.adminRules }),
    onError: (e) => toast.error(errMessage(e)),
  })

  const test = useMutation({
    mutationFn: (id: string) => testNotificationRule(id),
    onSuccess: () => toast.success(cs.admin.testSent),
    onError: (e) => toast.error(errMessage(e)),
  })

  if (editing) {
    return (
      <RuleEditor
        draft={editing}
        catalog={catalog}
        saving={save.isPending}
        onChange={setEditing}
        onCancel={() => setEditing(null)}
        onSave={() => save.mutate(editing)}
      />
    )
  }

  const items = rules.data?.items ?? []

  return (
    <section className="space-y-3 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex-1">
          <h2 className="text-base font-extrabold">{cs.admin.rulesHeading}</h2>
          <p className="text-[12.5px] text-muted">{cs.admin.rulesHint}</p>
        </div>
        <Button
          variant="primary"
          size="sm"
          disabled={!online}
          title={online ? undefined : cs.offline.writeBlocked}
          onClick={() => setEditing({ audience: { scope: 'all' }, coalesce_window_seconds: 60, enabled: true })}
        >
          <Plus size={14} aria-hidden />
          <span className="ml-1.5">{cs.admin.newRule}</span>
        </Button>
      </div>

      {rules.isPending && <p className="text-sm text-muted">{cs.common.loading}</p>}

      {!rules.isPending && items.length === 0 && (
        <div className="rounded-xl border border-dashed border-border-strong bg-s2 p-6 text-center">
          <div className="font-bold">{cs.admin.rulesEmpty}</div>
          <p className="mt-1 text-[13px] text-muted text-pretty">{cs.admin.rulesEmptyHint}</p>
        </div>
      )}

      <ul className="space-y-2">
        {items.map((r) => (
          <li key={r.id} className="rounded-xl border border-border bg-s2 p-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="flex-1 text-[14px] font-bold">{r.name}</span>
              <button
                type="button"
                role="switch"
                aria-checked={r.enabled}
                aria-label={cs.admin.enabledLabel}
                disabled={!online || toggle.isPending}
                onClick={() => toggle.mutate(r)}
                className={cn(
                  'h-6 w-11 flex-none rounded-full border transition-colors',
                  r.enabled ? 'border-accent bg-accent' : 'border-border-strong bg-s3',
                  !online && 'opacity-50',
                )}
              >
                <span
                  className={cn(
                    'block h-4 w-4 rounded-full bg-s1 transition-transform',
                    r.enabled ? 'translate-x-6' : 'translate-x-1',
                  )}
                />
              </button>
            </div>
            {/* The human phrase, never the raw key, is the primary label. */}
            <div className="mt-1 text-[12.5px] text-muted">{triggerPhrase(r, catalog)}</div>
            {(r.conditions || (r.active_from_local && r.active_to_local)) && (
              <div className="mt-0.5 text-[12px] text-subtle">
                {[
                  conditionsPhrase(r.conditions, catalog) &&
                    `${cs.admin.ruleConditionsBadge}: ${conditionsPhrase(r.conditions, catalog)}`,
                  r.active_from_local &&
                    r.active_to_local &&
                    `${cs.admin.ruleWindowBadge} ${r.active_from_local}–${r.active_to_local}`,
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </div>
            )}
            <div className="mt-2 flex flex-wrap gap-1.5">
              <Button size="sm" disabled={!online} onClick={() => setEditing(r)}>
                Upravit
              </Button>
              <Button size="sm" disabled={!online} loading={test.isPending} onClick={() => test.mutate(r.id)}>
                {cs.admin.sendTest}
              </Button>
              <Button
                size="sm"
                variant="danger"
                disabled={!online}
                onClick={() => {
                  if (window.confirm(cs.admin.deleteConfirm)) remove.mutate(r.id)
                }}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

function RuleEditor({
  draft,
  catalog,
  saving,
  onChange,
  onCancel,
  onSave,
}: {
  draft: Partial<NotificationRule>
  catalog?: NotificationCatalog
  saving: boolean
  onChange: (d: Partial<NotificationRule>) => void
  onCancel: () => void
  onSave: () => void
}) {
  const grouped = useMemo(() => groupActions(catalog?.actions ?? []), [catalog])
  // A rule with no trigger cannot be saved: the server refuses it, and for an
  // EXISTING rule the patch would simply omit the pair and keep the old trigger —
  // a 200 for a change that never happened. Say so on the field instead.
  const hasTrigger = !!(draft.action_key || draft.action_prefix)
  const audience = draft.audience ?? { scope: 'all' as const }
  // A half-filled window would be a 422; flag it on the field and hold save.
  const windowIncomplete = !!draft.active_from_local !== !!draft.active_to_local
  // Resolve the stored trigger back to a catalog entry. A rule saved before the
  // module travelled with the key has no filter_module, so fall back to matching
  // on the key alone rather than showing an empty select for a rule that has one.
  const selectedAction = useMemo(() => {
    if (!draft.action_key) return undefined
    const actions = catalog?.actions ?? []
    return (
      actions.find((a) => a.key === draft.action_key && a.module === draft.filter_module) ??
      actions.find((a) => a.key === draft.action_key)
    )
  }, [catalog, draft.action_key, draft.filter_module])

  return (
    <section className="space-y-4 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <h2 className="text-base font-extrabold">{draft.id ? cs.admin.tabRules : cs.admin.newRule}</h2>

      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.ruleName}</span>
        <Input value={draft.name ?? ''} onChange={(e) => onChange({ ...draft, name: e.target.value })} />
      </label>

      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.ruleTrigger}</span>
        <select
          value={selectedAction ? actionValue(selectedAction.module, selectedAction.key) : ''}
          onChange={(e) => {
            const picked = parseActionValue(e.target.value)
            onChange({
              ...draft,
              action_key: picked?.key ?? null,
              action_prefix: null,
              // The module is half the identity of a trigger, not decoration:
              // without it the rule matches the bare verb in EVERY module.
              filter_module: picked?.module ?? null,
            })
          }}
          className="h-10 w-full rounded-md border border-border bg-s2 px-3 text-sm text-fg"
        >
          <option value="">{cs.admin.ruleTriggerPlaceholder}</option>
          {grouped.map(([module, actions]) => (
            <optgroup key={module} label={moduleLabel(module)}>
              {actions.map((a) => (
                <option key={`${a.module}.${a.key}`} value={actionValue(a.module, a.key)}>
                  {a.label ?? a.key}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
        {draft.action_key && (
          // Honest about what actually fires, without making the key the label.
          <span className="mt-1 block font-mono text-[11px] text-subtle">{draft.action_key}</span>
        )}
        {!draft.action_key && draft.action_prefix && (
          // A prefix rule was made through the API; this editor offers exact keys
          // only, so show what it reacts to rather than an empty-looking select.
          <span className="mt-1 block font-mono text-[11px] text-subtle">{draft.action_prefix}*</span>
        )}
        {!hasTrigger && (
          <span className="mt-1 block text-[12px] text-danger">{cs.admin.ruleTriggerRequired}</span>
        )}
      </label>

      <Composer
        context="trigger"
        catalog={catalog}
        title={draft.title_template ?? ''}
        body={draft.body_template ?? ''}
        onTitleChange={(v) => onChange({ ...draft, title_template: v })}
        onBodyChange={(v) => onChange({ ...draft, body_template: v })}
        bodyPlaceholder={cs.admin.ruleBodyPlaceholder}
      />

      <ConditionsBuilder
        context="trigger"
        catalog={catalog}
        value={draft.conditions ?? null}
        onChange={(c) => onChange({ ...draft, conditions: c })}
      />

      <div>
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.activeWindow}</span>
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-1.5">
            <span className="text-[12px] text-muted">{cs.admin.activeFrom}</span>
            <Input
              type="time"
              value={draft.active_from_local ?? ''}
              onChange={(e) => onChange({ ...draft, active_from_local: e.target.value || null })}
              className="w-28 font-mono"
            />
          </label>
          <label className="flex items-center gap-1.5">
            <span className="text-[12px] text-muted">{cs.admin.activeTo}</span>
            <Input
              type="time"
              value={draft.active_to_local ?? ''}
              onChange={(e) => onChange({ ...draft, active_to_local: e.target.value || null })}
              className="w-28 font-mono"
            />
          </label>
        </div>
        {windowIncomplete && (
          <span className="mt-1 block text-[12px] text-danger">{cs.admin.activeWindowIncomplete}</span>
        )}
        <span className="mt-1 block text-[12px] text-muted text-pretty">{cs.admin.activeWindowHint}</span>
      </div>

      <AudiencePicker
        value={audience}
        onChange={(a) => onChange({ ...draft, audience: a })}
        members={catalog?.members ?? []}
      />

      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.coalesce}</span>
        <div className="flex items-center gap-2">
          <Input
            type="number"
            min={0}
            // Clamped to the same 24h the server enforces: past ~9.2e9 seconds the
            // backend's seconds→Duration multiply overflows negative and the rule
            // would fire on EVERY event — the exact opposite of what was asked for.
            max={MAX_COALESCE_SECONDS}
            value={draft.coalesce_window_seconds ?? 60}
            onChange={(e) =>
              onChange({
                ...draft,
                coalesce_window_seconds: clampCoalesce(Number(e.target.value)),
              })
            }
            className="w-24 font-mono"
          />
          <span className="text-[12.5px] text-muted">s</span>
        </div>
        <span className="mt-1 block text-[12px] text-muted text-pretty">
          {(draft.coalesce_window_seconds ?? 60) === 0 ? cs.admin.coalesceOff : cs.admin.coalesceHint}
        </span>
      </label>

      <label className="flex items-start gap-2.5">
        <input
          type="checkbox"
          className="mt-1 h-4 w-4"
          // exclude_actor inverted: the UI asks the positive question, and the
          // default is ON because v5 notifies everyone including the actor (D66).
          checked={!(draft.exclude_actor ?? false)}
          onChange={(e) => onChange({ ...draft, exclude_actor: !e.target.checked })}
        />
        <span>
          <span className="block text-[13.5px] font-semibold">{cs.admin.notifyActor}</span>
          <span className="block text-[12px] text-muted text-pretty">{cs.admin.notifyActorHint}</span>
        </span>
      </label>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="primary"
          loading={saving}
          disabled={
            !hasTrigger || !audienceValid(audience) || !conditionsValid(draft.conditions ?? null) || windowIncomplete
          }
          onClick={onSave}
        >
          {saving ? cs.admin.saving : cs.admin.save}
        </Button>
        <Button onClick={onCancel}>{cs.admin.cancel}</Button>
      </div>
    </section>
  )
}

// ---- Souhrny ----

function SummariesTab({ catalog }: { catalog?: NotificationCatalog }) {
  const online = useOnline()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Partial<NotificationSchedule> | null>(null)

  const schedules = useQuery({
    queryKey: qk.adminSchedules,
    queryFn: () => listNotificationSchedules({ limit: 100 }),
  })

  const save = useMutation({
    mutationFn: (draft: Partial<NotificationSchedule>) => {
      const body = {
        name: draft.name ?? '',
        schedule: draft.schedule ?? { time_local: '08:00', days: { preset: 'daily' } },
        audience: draft.audience ?? { scope: 'all' },
        title_template: draft.title_template ?? '',
        body_template: draft.body_template ?? '',
        enabled: draft.enabled ?? true,
        // The full block travels every save; no rows ⇒ null, which clears a
        // previously stored one (same three states as a rule patch).
        conditions: conditionsForSave(draft.conditions ?? null),
      }
      return draft.id ? updateNotificationSchedule(draft.id, body) : createNotificationSchedule(body)
    },
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: qk.adminSchedules })
    },
    onError: (e) => toast.error(errMessage(e)),
  })

  const toggle = useMutation({
    mutationFn: (s: NotificationSchedule) => updateNotificationSchedule(s.id, { enabled: !s.enabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: qk.adminSchedules }),
    onError: (e) => toast.error(errMessage(e)),
  })

  const remove = useMutation({
    mutationFn: (id: string) => deleteNotificationSchedule(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: qk.adminSchedules }),
    onError: (e) => toast.error(errMessage(e)),
  })

  const test = useMutation({
    mutationFn: (id: string) => testNotificationSchedule(id),
    onSuccess: () => toast.success(cs.admin.testSent),
    onError: (e) => toast.error(errMessage(e)),
  })

  if (editing) {
    return (
      <SummaryEditor
        draft={editing}
        catalog={catalog}
        saving={save.isPending}
        onChange={setEditing}
        onCancel={() => setEditing(null)}
        onSave={() => save.mutate(editing)}
      />
    )
  }

  const items = schedules.data?.items ?? []

  return (
    <section className="space-y-3 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex-1">
          <h2 className="text-base font-extrabold">{cs.admin.summariesHeading}</h2>
          <p className="text-[12.5px] text-muted">{cs.admin.summariesHint}</p>
        </div>
        <Button
          variant="primary"
          size="sm"
          disabled={!online}
          title={online ? undefined : cs.offline.writeBlocked}
          onClick={() =>
            setEditing({
              schedule: { time_local: '08:00', days: { preset: 'daily' } },
              audience: { scope: 'all' },
              enabled: true,
            })
          }
        >
          <Plus size={14} aria-hidden />
          <span className="ml-1.5">{cs.admin.newSummary}</span>
        </Button>
      </div>

      {schedules.isPending && <p className="text-sm text-muted">{cs.common.loading}</p>}

      {!schedules.isPending && items.length === 0 && (
        <div className="rounded-xl border border-dashed border-border-strong bg-s2 p-6 text-center">
          <div className="font-bold">{cs.admin.summariesEmpty}</div>
          <p className="mt-1 text-[13px] text-muted text-pretty">{cs.admin.summariesEmptyHint}</p>
        </div>
      )}

      <ul className="space-y-2">
        {items.map((s) => (
          <li key={s.id} className="rounded-xl border border-border bg-s2 p-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="flex-1 text-[14px] font-bold">{s.name}</span>
              <button
                type="button"
                role="switch"
                aria-checked={s.enabled}
                aria-label={cs.admin.enabledLabel}
                disabled={!online || toggle.isPending}
                onClick={() => toggle.mutate(s)}
                className={cn(
                  'h-6 w-11 flex-none rounded-full border transition-colors',
                  s.enabled ? 'border-accent bg-accent' : 'border-border-strong bg-s3',
                  !online && 'opacity-50',
                )}
              >
                <span
                  className={cn(
                    'block h-4 w-4 rounded-full bg-s1 transition-transform',
                    s.enabled ? 'translate-x-6' : 'translate-x-1',
                  )}
                />
              </button>
            </div>
            {/* The schedule phrase is rendered SERVER-side so the list and the
                ticker can never disagree about what a pattern means. */}
            <div className="mt-1 text-[12.5px] text-muted">{s.description}</div>
            {conditionsPhrase(s.conditions, catalog) && (
              <div className="mt-0.5 text-[12px] text-subtle">
                {cs.admin.ruleConditionsBadge}: {conditionsPhrase(s.conditions, catalog)}
              </div>
            )}
            <div className="mt-2 flex flex-wrap gap-1.5">
              <Button size="sm" disabled={!online} onClick={() => setEditing(s)}>
                Upravit
              </Button>
              <Button size="sm" disabled={!online} loading={test.isPending} onClick={() => test.mutate(s.id)}>
                {cs.admin.sendTest}
              </Button>
              <Button
                size="sm"
                variant="danger"
                disabled={!online}
                onClick={() => {
                  if (window.confirm(cs.admin.deleteConfirm)) remove.mutate(s.id)
                }}
              >
                <Trash2 size={14} aria-hidden />
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}

function SummaryEditor({
  draft,
  catalog,
  saving,
  onChange,
  onCancel,
  onSave,
}: {
  draft: Partial<NotificationSchedule>
  catalog?: NotificationCatalog
  saving: boolean
  onChange: (d: Partial<NotificationSchedule>) => void
  onCancel: () => void
  onSave: () => void
}) {
  const schedule = draft.schedule ?? { time_local: '08:00', days: { preset: 'daily' as const } }
  const audience = draft.audience ?? { scope: 'all' as const }

  return (
    <section className="space-y-4 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <h2 className="text-base font-extrabold">{draft.id ? cs.admin.tabSummaries : cs.admin.newSummary}</h2>

      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{cs.admin.summaryName}</span>
        <Input value={draft.name ?? ''} onChange={(e) => onChange({ ...draft, name: e.target.value })} />
      </label>

      <ScheduleBuilder
        timeLocal={schedule.time_local}
        days={schedule.days}
        onTimeChange={(v) => onChange({ ...draft, schedule: { ...schedule, time_local: v } })}
        onDaysChange={(d) => onChange({ ...draft, schedule: { ...schedule, days: d } })}
      />

      <Composer
        context="summary"
        catalog={catalog}
        title={draft.title_template ?? ''}
        body={draft.body_template ?? ''}
        onTitleChange={(v) => onChange({ ...draft, title_template: v })}
        onBodyChange={(v) => onChange({ ...draft, body_template: v })}
      />
      <p className="text-[12px] text-muted text-pretty">{cs.admin.perRecipientNote}</p>

      <ConditionsBuilder
        context="summary"
        catalog={catalog}
        value={draft.conditions ?? null}
        onChange={(c) => onChange({ ...draft, conditions: c })}
      />

      <AudiencePicker
        value={audience}
        onChange={(a) => onChange({ ...draft, audience: a })}
        members={catalog?.members ?? []}
      />

      <div className="flex flex-wrap gap-2">
        <Button
          variant="primary"
          loading={saving}
          disabled={!audienceValid(audience) || !conditionsValid(draft.conditions ?? null)}
          onClick={onSave}
        >
          {saving ? cs.admin.saving : cs.admin.save}
        </Button>
        <Button onClick={onCancel}>{cs.admin.cancel}</Button>
      </div>
    </section>
  )
}

// ---- Doručení ----

function DeliveriesTab() {
  const [kind, setKind] = useState('')
  const [status, setStatus] = useState('')

  const filters = { kind: kind || undefined, status: status || undefined, limit: 50 }
  const deliveries = useQuery({
    queryKey: qk.adminDeliveries(filters),
    queryFn: () => listDeliveries(filters),
  })

  const items = deliveries.data?.items ?? []

  return (
    <section className="space-y-3 rounded-xl border border-border bg-s1 p-4 md:p-5">
      <div>
        <h2 className="text-base font-extrabold">{cs.admin.deliveriesHeading}</h2>
        {/* Says plainly that this is NOT the audit log — it borrows the Log's
            chrome, so the copy has to carry the difference. */}
        <p className="text-[12.5px] text-muted text-pretty">{cs.admin.deliveriesHint}</p>
      </div>

      <div className="flex flex-wrap gap-2">
        <FilterSelect
          label={cs.admin.colKind}
          value={kind}
          onChange={setKind}
          options={[
            ['broadcast', cs.admin.kindBroadcast],
            ['trigger', cs.admin.kindTrigger],
            ['schedule', cs.admin.kindSchedule],
            ['test', cs.admin.kindTest],
            // v10. Widening notification_deliveries' CHECK (08003) was done so the
            // log could answer "did the chat push go out?" separately from "did the
            // rule fire?" — which needs the filter that asks it.
            ['chat', cs.admin.kindChat],
          ]}
        />
        <FilterSelect
          label={cs.admin.colStatus}
          value={status}
          onChange={setStatus}
          options={[
            ['sent', cs.admin.statusSent],
            ['failed', cs.admin.statusFailed],
            ['expired', cs.admin.statusExpired],
          ]}
        />
      </div>

      {deliveries.isPending && <p className="text-sm text-muted">{cs.common.loading}</p>}

      {!deliveries.isPending && items.length === 0 && (
        <p className="rounded-xl border border-dashed border-border-strong bg-s2 p-6 text-center text-sm text-muted">
          {cs.admin.deliveriesEmpty}
        </p>
      )}

      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[32rem] text-left text-[12.5px]">
            <thead className="font-mono text-[10px] uppercase tracking-wide text-subtle">
              <tr>
                <th className="py-1.5 pr-3">{cs.admin.colTime}</th>
                <th className="py-1.5 pr-3">{cs.admin.colKind}</th>
                <th className="py-1.5 pr-3">{cs.admin.colRecipient}</th>
                <th className="py-1.5">{cs.admin.colStatus}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((d) => (
                <tr key={d.id} className="border-t border-border">
                  <td className="py-1.5 pr-3 font-mono text-[11.5px] text-muted">{formatTime(d.ts)}</td>
                  <td className="py-1.5 pr-3">{kindLabel(d.kind)}</td>
                  <td className="py-1.5 pr-3">{d.user_label}</td>
                  <td className="py-1.5">
                    <StatusChip status={d.status} error={d.error} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: [string, string][]
}) {
  return (
    <label className="flex items-center gap-1.5">
      <span className="text-[12px] text-muted">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 rounded-md border border-border bg-s2 px-2 text-[13px] text-fg"
      >
        <option value="">{cs.admin.filterAll}</option>
        {options.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
    </label>
  )
}

/** StatusChip carries a TEXT label, never colour alone (accessibility pass). */
function StatusChip({ status, error }: { status: DeliveryStatus; error: string | null }) {
  const map: Record<DeliveryStatus, { label: string; className: string }> = {
    sent: { label: cs.admin.statusSent, className: 'border-good/45 text-good' },
    failed: { label: cs.admin.statusFailed, className: 'border-warn/45 text-warn' },
    expired: { label: cs.admin.statusExpired, className: 'border-border text-muted' },
  }
  const s = map[status]
  return (
    <span
      title={error ?? undefined}
      className={cn('inline-flex h-6 items-center rounded-full border px-2 text-[11.5px] font-semibold', s.className)}
    >
      {s.label}
    </span>
  )
}

// ---- helpers ----

function errMessage(e: unknown): string {
  return apiErrorMessage(e, cs.admin.saveError)
}

// A trigger is identified by MODULE + KEY, never by the key alone: action keys
// are bare verbs and two modules may declare the same one (notes' `folder.create`
// against a future module's). `|` occurs in neither half, so it is a safe joiner
// for carrying the pair through a single <option value>.
const ACTION_VALUE_SEP = '|'

function actionValue(module: string, key: string): string {
  return `${module}${ACTION_VALUE_SEP}${key}`
}

function parseActionValue(value: string): { module: string; key: string } | null {
  const at = value.indexOf(ACTION_VALUE_SEP)
  if (at < 0) return null
  const key = value.slice(at + 1)
  return key ? { module: value.slice(0, at), key } : null
}

function groupActions(actions: ActionDescriptor[]): [string, ActionDescriptor[]][] {
  const byModule = new Map<string, ActionDescriptor[]>()
  for (const a of actions) {
    const list = byModule.get(a.module) ?? []
    list.push(a)
    byModule.set(a.module, list)
  }
  return [...byModule.entries()]
}

function moduleLabel(module: string): string {
  const labels: Record<string, string> = {
    todo: cs.nav.ukoly,
    events: cs.nav.okno,
    notes: cs.nav.poznamky,
    documents: cs.nav.dokumenty,
    finance: cs.nav.finance,
    logging: cs.nav.log,
    platform: 'Přihlášení a oznámení',
    dashboard: cs.nav.nastenka,
    admin: cs.admin.title,
  }
  return labels[module] ?? module
}

/** triggerPhrase shows the human Czech phrase for a rule's trigger. The module is
 *  part of the lookup because two modules may label the same bare verb
 *  differently; a rule with no module falls back to the first match. */
function triggerPhrase(rule: NotificationRule, catalog?: NotificationCatalog): string {
  if (rule.action_prefix) return `Skupina akcí: ${rule.action_prefix}*`
  const actions = catalog?.actions ?? []
  const match =
    actions.find((a) => a.key === rule.action_key && a.module === rule.filter_module) ??
    actions.find((a) => a.key === rule.action_key)
  return match?.label ?? rule.action_key ?? '—'
}

function kindLabel(kind: string): string {
  const map: Record<string, string> = {
    broadcast: cs.admin.kindBroadcast,
    trigger: cs.admin.kindTrigger,
    schedule: cs.admin.kindSchedule,
    test: cs.admin.kindTest,
    chat: cs.admin.kindChat,
  }
  // The fallback prints the raw key, which is English in a Czech-only column — it
  // is a backstop for a kind this build has not heard of, not a place to leave one.
  return map[kind] ?? kind
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getDate()}. ${d.getMonth() + 1}. ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}
