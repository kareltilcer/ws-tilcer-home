// The audience picker (HANDOFF-design §v5 §6).
//
// One compact control that EXPANDS, never a wizard: Všem (the default) / Podle
// role / Vybraným lidem, with a live recipient echo so an admin always knows how
// wide the blast radius is before pressing Odeslat.

import { cs } from '@/i18n/cs'
import { cn } from '@/lib/utils'
import { count } from '@/i18n/plural'
import { PLURAL } from '@/i18n/plural'
import type { Audience, HouseholdMember } from '@/api/types'

const ROLES = [
  { key: 'admin', label: 'Správci' },
  { key: 'editor', label: 'Editoři' },
  { key: 'reader', label: 'Čtenáři' },
] as const

/** audienceValid mirrors push.Audience.Valid() on the server: an explicitly EMPTY
 *  role or user selection names nobody and is refused with a 422.
 *
 *  The picker can produce exactly that — switching to "Podle role" before ticking
 *  a role — so every screen that submits an audience checks this and disables its
 *  button. `RecipientEcho` already SAYS the selection is empty; letting the
 *  request go anyway turned a state the UI had diagnosed into a server error. */
export function audienceValid(a: Audience): boolean {
  switch (a.scope) {
    case 'roles':
      return (a.roles?.length ?? 0) > 0
    case 'users':
      return (a.users?.length ?? 0) > 0
    default:
      // "Všem" with nobody subscribed is the household's state, not an error.
      return true
  }
}

export function AudiencePicker({
  value,
  onChange,
  members,
  disabled,
}: {
  value: Audience
  onChange: (a: Audience) => void
  members: HouseholdMember[]
  disabled?: boolean
}) {
  const scope = value.scope ?? 'all'

  const toggleRole = (role: string) => {
    const roles = new Set(value.roles ?? [])
    if (roles.has(role)) roles.delete(role)
    else roles.add(role)
    onChange({ ...value, scope: 'roles', roles: [...roles] })
  }

  const toggleUser = (userID: string) => {
    const users = new Set(value.users ?? [])
    if (users.has(userID)) users.delete(userID)
    else users.add(userID)
    onChange({ ...value, scope: 'users', users: [...users] })
  }

  return (
    <div className="space-y-2">
      <div className="text-[12.5px] font-semibold text-muted">{cs.admin.audience}</div>

      <div className="flex flex-wrap gap-1.5">
        {(
          [
            ['all', cs.admin.audienceAll],
            ['roles', cs.admin.audienceRoles],
            ['users', cs.admin.audienceUsers],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            disabled={disabled}
            onClick={() => onChange({ scope: key, roles: value.roles, users: value.users })}
            className={cn(
              'inline-flex min-h-[36px] items-center rounded-lg border px-3 text-[13px] font-semibold',
              scope === key ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-s2 text-muted',
              disabled && 'cursor-not-allowed opacity-50',
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {scope === 'roles' && (
        <div className="flex flex-wrap gap-1.5 pl-0.5">
          {ROLES.map((r) => {
            const on = (value.roles ?? []).includes(r.key)
            return (
              <button
                key={r.key}
                type="button"
                disabled={disabled}
                aria-pressed={on}
                onClick={() => toggleRole(r.key)}
                className={cn(
                  'inline-flex min-h-[32px] items-center rounded-lg border px-2.5 text-[12.5px] font-semibold',
                  on ? 'border-accent text-accent' : 'border-border text-muted',
                )}
              >
                {r.label}
              </button>
            )
          })}
        </div>
      )}

      {scope === 'users' && (
        <div className="flex flex-wrap gap-1.5 pl-0.5">
          {members.length === 0 && <span className="text-[12.5px] text-subtle">Zatím nikdo.</span>}
          {members.map((m) => {
            const on = (value.users ?? []).includes(m.user_id)
            return (
              <button
                key={m.user_id}
                type="button"
                disabled={disabled}
                aria-pressed={on}
                onClick={() => toggleUser(m.user_id)}
                className={cn(
                  'inline-flex min-h-[32px] items-center gap-1.5 rounded-lg border px-2.5 text-[12.5px] font-semibold',
                  on ? 'border-accent text-accent' : 'border-border text-muted',
                )}
              >
                {m.display_name}
                {/* A member with no device can be selected but will receive
                    nothing — say so rather than silently dropping them. */}
                {m.subscriptions === 0 && <span className="text-[10px] text-subtle">bez zařízení</span>}
              </button>
            )
          })}
        </div>
      )}

      <RecipientEcho audience={value} members={members} />
    </div>
  )
}

/** RecipientEcho answers "how many people will this actually reach?" before the
 *  send, using the same rules the server applies. */
function RecipientEcho({ audience, members }: { audience: Audience; members: HouseholdMember[] }) {
  const scope = audience.scope ?? 'all'
  const matched = members.filter((m) => {
    if (scope === 'all') return m.subscriptions > 0
    if (scope === 'roles') {
      const want = audience.roles ?? []
      return m.roles.some((r) => r === '*' || want.includes(r))
    }
    return (audience.users ?? []).includes(m.user_id)
  })
  const withDevices = matched.filter((m) => m.subscriptions > 0)
  const devices = withDevices.reduce((n, m) => n + m.subscriptions, 0)

  if (scope !== 'all' && matched.length === 0) {
    return <p className="text-[12px] text-danger">{cs.admin.audienceEmpty}</p>
  }
  if (withDevices.length === 0) {
    // Composing is allowed with nobody subscribed — it is a household state, not
    // a validation failure — but it must not look like a working send.
    return <p className="text-[12px] text-warn text-pretty">{cs.admin.nobodySubscribed}</p>
  }
  return (
    <p className="text-[12px] text-muted">
      {scope === 'all' ? `${cs.admin.audienceEchoAll} · ` : ''}
      {count(withDevices.length, PLURAL.people)} · {count(devices, PLURAL.devices)}
    </p>
  )
}
