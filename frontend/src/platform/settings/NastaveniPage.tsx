// Nastavení → Oznámení (HANDOFF-design §v5 §8; PRD FR-P1–P5, §V5-7).
//
// This is the ONE notification surface every role sees, `reader` included: a
// reader has no Administrace at all, but subscribes and mutes exactly like
// anyone else. That split is deliberate and it is what this screen makes legible.
//
// Everything here is per DEVICE. The panel says so twice — a badge and a hint —
// because a member with a phone and a laptop manages each separately, and v5 has
// no cross-device console to fall back on.

import { useMutation, type UseMutationResult } from '@tanstack/react-query'
import { BellRing, Check, Download, Moon, Smartphone, Sun, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/ui'
import { ScreenHeader } from '@/components/common/states'
import { useTheme } from '@/theme/theme'
import { usePushDevice, usePushPreferences } from '@/platform/push/usePush'
import { useInstallPrompt, useOnline } from '@/platform/pwa/offline'
import { sendPushTest } from '@/api/endpoints'
import { ApiError } from '@/api/client'
import type { PushCategories, PushTestResult } from '@/api/types'

export function NastaveniPage() {
  const { theme, toggle } = useTheme()
  const online = useOnline()
  const { canInstall, install } = useInstallPrompt()
  const { state, subscribe, unsubscribe } = usePushDevice()
  const { query: prefsQuery, update } = usePushPreferences()

  // A self-test that only claims to have sent something is worse than no button
  // at all: it turns "my device is broken" into "the app says it works". So the
  // button makes a real send to this account's own endpoints and reports what
  // the push service actually accepted.
  const selfTest = useMutation({
    mutationFn: sendPushTest,
    onSuccess: (res) => {
      if (res.sent > 0) toast.success(cs.settings.selfTestSent)
      else toast.error(cs.settings.selfTestFailed)
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.detail || cs.settings.selfTestFailed : cs.settings.selfTestFailed),
  })

  const prefs = prefsQuery.data
  const masterOn = prefs?.enabled ?? true

  const setCategory = (key: keyof PushCategories, value: boolean) =>
    update.mutate({ categories: { [key]: value } })

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <ScreenHeader title={cs.settings.title} subtitle={cs.settings.subtitle} />

      {/* ---- Oznámení (this device) ---- */}
      <section className="rounded-xl border border-border bg-s1 p-4 md:p-5">
        <div className="mb-3 flex flex-wrap items-center gap-2.5">
          <h2 className="text-base font-extrabold">{cs.settings.notifications}</h2>
          <span className="inline-flex h-6 items-center rounded-full border border-border px-2.5 font-mono text-[10px] uppercase tracking-wide text-subtle">
            {cs.settings.thisDevice}
          </span>
          <span className="ml-auto text-[11.5px] text-subtle">{cs.settings.thisDeviceHint}</span>
        </div>

        <DeviceState
          state={state}
          online={online}
          onEnable={() => void subscribe()}
          onDisable={() => void unsubscribe()}
          selfTest={selfTest}
        />

        {/* The mutes are useful even before subscribing — they describe what this
            account will accept once a device is on. */}
        <div className="mt-4 space-y-2.5">
          <ToggleRow
            label={cs.settings.masterLabel}
            hint={cs.settings.masterHint}
            checked={masterOn}
            disabled={!online || update.isPending}
            onChange={(v) => update.mutate({ enabled: v })}
          />
          <div className={cn('space-y-2.5 pl-3', !masterOn && 'opacity-50')}>
            <ToggleRow
              label={cs.settings.catBroadcast}
              hint={cs.settings.catBroadcastHint}
              checked={prefs?.categories.broadcast ?? true}
              disabled={!masterOn || !online || update.isPending}
              onChange={(v) => setCategory('broadcast', v)}
            />
            <ToggleRow
              label={cs.settings.catTriggers}
              hint={cs.settings.catTriggersHint}
              checked={prefs?.categories.triggers ?? true}
              disabled={!masterOn || !online || update.isPending}
              onChange={(v) => setCategory('triggers', v)}
            />
            <ToggleRow
              label={cs.settings.catSummaries}
              hint={cs.settings.catSummariesHint}
              checked={prefs?.categories.summaries ?? true}
              disabled={!masterOn || !online || update.isPending}
              onChange={(v) => setCategory('summaries', v)}
            />
            {/* v10. The backend has carried cat_chat since 02004 — the column, the
                category, the patch field and the delivery-log kind — but this row
                was missing, so the app-wide chat mute existed only in the API and
                a member's one recourse was muting every conversation by hand. */}
            <ToggleRow
              label={cs.settings.catChat}
              hint={cs.settings.catChatHint}
              checked={prefs?.categories.chat ?? true}
              disabled={!masterOn || !online || update.isPending}
              onChange={(v) => setCategory('chat', v)}
            />
          </div>
        </div>
      </section>

      {/* ---- Vzhled ---- */}
      <section className="rounded-xl border border-border bg-s1 p-4 md:p-5">
        <h2 className="mb-3 text-base font-extrabold">{cs.settings.appearance}</h2>
        <div className="flex items-center gap-3">
          <span className="flex-1 text-sm">{cs.settings.theme}</span>
          <Button variant="secondary" onClick={toggle}>
            {theme === 'dark' ? <Sun size={16} aria-hidden /> : <Moon size={16} aria-hidden />}
            <span className="ml-2">{theme === 'dark' ? 'Světlý' : 'Tmavý'}</span>
          </Button>
        </div>
      </section>

      {/* ---- Aplikace (install + offline explanation) ---- */}
      <section className="rounded-xl border border-border bg-s1 p-4 md:p-5">
        <h2 className="mb-3 text-base font-extrabold">{cs.settings.app}</h2>
        {canInstall && (
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Button variant="secondary" onClick={() => void install()}>
              <Download size={16} aria-hidden />
              <span className="ml-2">{cs.settings.install}</span>
            </Button>
          </div>
        )}
        <p className="text-[12.5px] text-muted text-pretty">{cs.settings.installHint}</p>
        <p className="mt-2 text-[12.5px] text-muted text-pretty">{cs.settings.offlineNote}</p>
      </section>
    </div>
  )
}

/** DeviceState renders the permission gauntlet: every outcome has a real state,
 *  and none of them is a dead button. */
function DeviceState({
  state,
  online,
  onEnable,
  onDisable,
  selfTest,
}: {
  state: ReturnType<typeof usePushDevice>['state']
  online: boolean
  onEnable: () => void
  onDisable: () => void
  selfTest: UseMutationResult<PushTestResult, Error, void, unknown>
}) {
  // The server has no VAPID keypair: nothing to subscribe to, household-wide.
  if (state.serverDisabled) {
    return (
      <Callout tone="warn" title={cs.settings.serverDisabledTitle} body={cs.settings.serverDisabledBody} />
    )
  }

  // No Push API — most often iOS Safari outside an installed PWA.
  if (state.support === 'unsupported') {
    return (
      <Callout tone="muted" title={cs.settings.unsupportedTitle} body={cs.settings.unsupportedBody} />
    )
  }

  // Blocked: the app cannot re-prompt, so the only honest move is to explain
  // where the browser's own setting lives.
  if (state.support === 'blocked') {
    return <Callout tone="warn" title={cs.settings.blockedTitle} body={cs.settings.blockedBody} />
  }

  if (state.subscribed) {
    return (
      <div className="space-y-2.5">
        <div className="flex items-center gap-2.5 rounded-xl border border-good/40 bg-good/10 px-3 py-2.5">
          <Check size={16} className="text-good" aria-hidden />
          <span className="flex-1 text-[13.5px] font-bold">{cs.settings.enabled}</span>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            disabled={!online || state.busy}
            loading={selfTest.isPending}
            // A real send to this account's own endpoints, mutes bypassed: the
            // notification arriving IS the confirmation.
            onClick={() => selfTest.mutate()}
          >
            <BellRing size={16} aria-hidden />
            <span className="ml-2">{cs.settings.selfTest}</span>
          </Button>
          <Button variant="secondary" disabled={!online || state.busy} onClick={onDisable}>
            {cs.settings.disable}
          </Button>
        </div>
        {selfTest.isSuccess && (
          <p className="text-[12.5px] text-muted" role="status">
            {selfTest.data.sent > 0 ? cs.settings.selfTestSent : cs.settings.selfTestFailed}
          </p>
        )}
      </div>
    )
  }

  // The priming step: the OS prompt fires only from this button, never on load.
  return (
    <div className="space-y-2.5">
      <p className="text-[13.5px] text-muted text-pretty">{cs.settings.primingBody}</p>
      <Button variant="primary" disabled={!online || state.busy} onClick={onEnable}>
        <Smartphone size={16} aria-hidden />
        <span className="ml-2">{cs.settings.enable}</span>
      </Button>
      {state.dismissed && <p className="text-[12.5px] text-subtle">{cs.settings.dismissed}</p>}
      {!online && <p className="text-[12.5px] text-muted">{cs.settings.offlineHint}</p>}
      {state.error && <p className="text-[12.5px] text-danger">{state.error}</p>}
    </div>
  )
}

function Callout({ tone, title, body }: { tone: 'warn' | 'muted'; title: string; body: string }) {
  return (
    <div
      className={cn(
        'flex gap-3 rounded-xl border p-3',
        // A blocked permission is a WARNING, not a destructive error — nothing
        // was lost, something needs fixing elsewhere.
        tone === 'warn' ? 'border-warn/45 bg-warn/10' : 'border-border bg-s2',
      )}
    >
      <TriangleAlert size={16} className={tone === 'warn' ? 'flex-none text-warn' : 'flex-none text-muted'} aria-hidden />
      <div>
        <div className="text-[13.5px] font-bold">{title}</div>
        <div className="mt-1 text-[12.5px] text-muted text-pretty">{body}</div>
      </div>
    </div>
  )
}

function ToggleRow({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string
  hint: string
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-border bg-s2 px-3.5 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-bold">{label}</div>
        <div className="text-[12px] text-muted">{hint}</div>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cn(
          // 44px minimum touch target (accessibility pass).
          'relative h-6 w-11 flex-none rounded-full border transition-colors',
          'after:absolute after:inset-y-0 after:-inset-x-2 after:content-[""]',
          checked ? 'border-accent bg-accent' : 'border-border-strong bg-s3',
          disabled && 'cursor-not-allowed opacity-50',
        )}
      >
        <span
          className={cn(
            'block h-4 w-4 rounded-full bg-s1 transition-transform',
            checked ? 'translate-x-6' : 'translate-x-1',
          )}
        />
      </button>
    </div>
  )
}
