// The notification composer (HANDOFF-design §v5 §2, hard problem 1).
//
// This is the module's payoff screen and its one genuinely novel interaction:
// free-form Czech interleaved with tokens that are PICKED from a catalog, never
// typed, plus a live preview that resolves those tokens to sample values so the
// admin reads real Czech instead of {{…}}.
//
// It is built once and specialised three ways by context — Rozeslat gets only
// time tokens, Pravidla gets event tokens, Souhrny gets metric tokens — because
// three near-identical composers would drift apart within a release.

import { useMemo, useRef, useState } from 'react'
import { Plus } from 'lucide-react'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { Button, Input, Textarea } from '@/components/ui/ui'
import type { MetricDescriptor, NotificationCatalog, TokenPalette } from '@/api/types'

export type ComposerContext = 'broadcast' | 'trigger' | 'summary'

interface ComposerProps {
  context: ComposerContext
  catalog?: NotificationCatalog
  title: string
  body: string
  onTitleChange: (v: string) => void
  onBodyChange: (v: string) => void
  titleLabel?: string
  bodyLabel?: string
  bodyPlaceholder?: string
  disabled?: boolean
}

export function Composer({
  context,
  catalog,
  title,
  body,
  onTitleChange,
  onBodyChange,
  titleLabel = cs.admin.composerTitle,
  bodyLabel = cs.admin.composerBody,
  bodyPlaceholder,
  disabled,
}: ComposerProps) {
  const bodyRef = useRef<HTMLTextAreaElement>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  // Which field a token insert lands in — whichever the admin touched last, so
  // "Vložit údaj" does the obvious thing instead of always targeting the body.
  const [lastFocused, setLastFocused] = useState<'title' | 'body'>('body')

  const palette = catalog?.tokens?.[context]
  const sample = useMemo(() => sampleValues(catalog), [catalog])

  /** insert drops a token at the caret of whichever field was last focused.
   *
   *  The change chips carry a `<pole>` the admin has to replace with a real field
   *  name — an unedited one is refused on save — so the caret lands ON that
   *  placeholder, selected, and the next keystroke replaces it. */
  const insert = (token: string) => {
    const wrapped = `{{${token}}}`
    const open = wrapped.indexOf('<')
    const close = wrapped.indexOf('>')
    const select = (at: number): [number, number] =>
      open >= 0 && close > open ? [at + open, at + close + 1] : [at + wrapped.length, at + wrapped.length]

    if (lastFocused === 'title') {
      const el = titleRef.current
      const at = el?.selectionStart ?? title.length
      onTitleChange(title.slice(0, at) + wrapped + title.slice(at))
      queueMicrotask(() => {
        el?.focus()
        el?.setSelectionRange(...select(at))
      })
      return
    }
    const el = bodyRef.current
    const at = el?.selectionStart ?? body.length
    onBodyChange(body.slice(0, at) + wrapped + body.slice(at))
    queueMicrotask(() => {
      el?.focus()
      el?.setSelectionRange(...select(at))
    })
  }

  return (
    <div className="space-y-3">
      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{titleLabel}</span>
        <Input
          ref={titleRef}
          value={title}
          disabled={disabled}
          onFocus={() => setLastFocused('title')}
          onChange={(e) => onTitleChange(e.target.value)}
        />
      </label>

      <label className="block">
        <span className="mb-1 block text-[12.5px] font-semibold text-muted">{bodyLabel}</span>
        <Textarea
          ref={bodyRef}
          value={body}
          rows={3}
          disabled={disabled}
          placeholder={bodyPlaceholder}
          onFocus={() => setLastFocused('body')}
          onChange={(e) => onBodyChange(e.target.value)}
        />
      </label>

      {palette && <TokenPicker palette={palette} catalog={catalog} disabled={disabled} onInsert={insert} />}

      <PreviewCard title={render(title, sample)} body={render(body, sample) || bodyPlaceholder || ''} />
    </div>
  )
}

/** TokenPicker offers the catalog-driven palette. Tokens are always picked from
 *  a list — an admin must never have to know that `reminder.complete` exists. */
function TokenPicker({
  palette,
  catalog,
  disabled,
  onInsert,
}: {
  palette: TokenPalette
  catalog?: NotificationCatalog
  disabled?: boolean
  onInsert: (token: string) => void
}) {
  const [open, setOpen] = useState(false)
  const metricLabels = useMemo(() => {
    const map = new Map<string, MetricDescriptor>()
    for (const m of catalog?.metrics ?? []) map.set(m.key, m)
    return map
  }, [catalog])

  const groups: { label: string; tokens: string[]; describe?: (t: string) => string }[] = []
  if (palette.time?.length) groups.push({ label: cs.admin.tokensTime, tokens: palette.time })
  if (palette.event?.length) groups.push({ label: cs.admin.tokensEvent, tokens: palette.event })
  if (palette.change?.length) groups.push({ label: cs.admin.tokensChange, tokens: palette.change })
  if (palette.metric?.length) {
    groups.push({
      label: cs.admin.tokensMetric,
      tokens: palette.metric,
      describe: (t) => {
        const m = metricLabels.get(t.replace(/^metric\./, ''))
        if (!m) return t
        return m.scope === 'personal' ? `${m.label} · osobní` : m.label
      },
    })
  }

  return (
    <div>
      <Button variant="secondary" size="sm" disabled={disabled} onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <Plus size={14} aria-hidden />
        <span className="ml-1.5">{cs.admin.insertToken}</span>
      </Button>

      {open && (
        <div className="mt-2 space-y-3 rounded-xl border border-border bg-s2 p-3">
          {groups.map((g) => (
            <div key={g.label}>
              <div className="mb-1.5 font-mono text-[10px] uppercase tracking-wide text-subtle">{g.label}</div>
              <div className="flex flex-wrap gap-1.5">
                {g.tokens.map((t) => (
                  <button
                    key={t}
                    type="button"
                    onClick={() => onInsert(t)}
                    className="inline-flex min-h-[32px] items-center rounded-lg border border-border-strong bg-s1 px-2.5 text-[12.5px] font-semibold hover:border-accent hover:text-accent"
                  >
                    {g.describe ? g.describe(t) : humanToken(t)}
                  </button>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** PreviewCard reads like an OS notification without pretending to BE one — the
 *  chrome is ours, only the shape is borrowed. */
function PreviewCard({ title, body }: { title: string; body: string }) {
  return (
    <div>
      <div className="mb-1.5 font-mono text-[10px] uppercase tracking-wide text-subtle">{cs.admin.preview}</div>
      <div className="flex gap-3 rounded-xl border border-border-strong bg-s3 p-3">
        <div className="grid h-9 w-9 flex-none place-items-center rounded-[10px] bg-accent font-extrabold text-accent-fg">
          h
        </div>
        <div className="min-w-0">
          <div className="truncate text-[13px] font-bold">{title || 'Home'}</div>
          <div className={cn('mt-0.5 text-[12.5px] text-muted', !body && 'italic')}>
            {body || 'Text oznámení…'}
          </div>
        </div>
      </div>
      <p className="mt-1.5 text-[11.5px] text-subtle text-pretty">{cs.admin.previewHint}</p>
    </div>
  )
}

// ---- preview rendering ----
//
// The preview runs the SAME substitution the server does (admin/template.go), on
// sample values. It is deliberately dumb: no conditionals, no expressions — if
// this file ever needs logic, the token palette has grown into a language and
// D61 has been broken.

// EXACTLY the server's pattern (admin/template.go tokenPattern). It deliberately
// excludes `<` and `>`, so an unedited `{{change.<pole>.old}}` chip renders in the
// preview as the raw text it would actually be delivered as — the preview has to
// show the mistake, not paper over it with a sample value the server can't produce.
const TOKEN_RE = /\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}/g

function render(template: string, values: Record<string, string>): string {
  return template.replace(TOKEN_RE, (_m, token: string) => values[token] ?? '—')
}

/** sampleValues mirrors the server's sample event so the preview shows real
 *  Czech, and metric tokens show a plausible number rather than a zero. */
function sampleValues(catalog?: NotificationCatalog): Record<string, string> {
  const now = new Date()
  const values: Record<string, string> = {
    now: `${now.getHours()}:${String(now.getMinutes()).padStart(2, '0')}`,
    date: `${now.getDate()}. ${now.getMonth() + 1}. ${now.getFullYear()}`,
    'event.summary': 'Karel přesunul kartu „Vynést koš“ do Hotovo',
    'event.action': 'card.move',
    'event.module': 'todo',
    'event.entity_type': 'card',
    'event.entity_id': '01a0…',
    'event.actor_label': 'Karel',
  }
  // Sample metric values are stable per key so the preview does not flicker.
  const samples = [3, 5, 2, 7, 4, 1, 6]
  ;(catalog?.metrics ?? []).forEach((m, i) => {
    values[`metric.${m.key}`] = String(samples[i % samples.length])
  })
  return values
}

/** humanToken renders a raw token for the picker chip. */
function humanToken(token: string): string {
  const labels: Record<string, string> = {
    now: 'Čas',
    date: 'Datum',
    'event.summary': 'Popis akce',
    'event.action': 'Kód akce',
    'event.module': 'Modul',
    'event.entity_type': 'Typ položky',
    'event.entity_id': 'ID položky',
    'event.actor_label': 'Kdo to udělal',
    'change.<pole>.old': 'Původní hodnota',
    'change.<pole>.new': 'Nová hodnota',
  }
  return labels[token] ?? token
}
