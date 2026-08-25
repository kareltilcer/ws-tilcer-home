import { Link } from 'react-router-dom'
import { Lock, Users } from 'lucide-react'
import type { Scope } from '@/api/types'
import { cn } from '@/lib/utils'
import { scopedRoute } from '@/lib/scope'
import { cs } from '@/i18n/cs'

/**
 * The root switcher — the whole interaction design of v9's privacy half
 * (PRD §V9-7, HANDOFF-design §v9 §1).
 *
 * Poznámky and Dokumenty each have two roots now. The design problem is NOT "how
 * do we show a private folder" — that part is a lock icon and an hour. It is
 * **how a person always knows which of the two trees they are standing in**, at
 * 375 px, in a hurry, one-handed, when the cost of being wrong is putting
 * something private in front of the whole household and there is no way to take
 * it back (D182: publish is one-way, and there is no unpublish).
 *
 * Two failure modes pull in opposite directions:
 *
 *   - a switcher so QUIET that somebody uploads a private document into the
 *     shared tree;
 *   - a switcher so LOUD that the shared tree — used ninety per cent of the time
 *     — feels like it is being guarded.
 *
 * ⚠ The resolution is PERSISTENT SHAPE, NOT A WARNING. A warning is read once;
 * shape is read every time. So the current root is carried by five things at
 * once, none of them dismissible: this switcher, the page heading, the tinted
 * tree container with its left rail, the breadcrumb's root segment, and the
 * search placeholder. Any one of them alone would be missable; together they
 * make a screenshot of one root unmistakable from a screenshot of the other.
 *
 * It is a pair of LINKS rather than a toggle because the scope lives in the URL
 * (see lib/scope.ts): there is no state to hold, the back button works, and a
 * pasted link lands where the sender was standing.
 */
export function RootSwitcher({
  scope,
  base,
  sharedLabel,
  privateLabel,
  className,
}: {
  scope: Scope
  /** routes.poznamky | routes.dokumenty */
  base: string
  sharedLabel: string
  privateLabel: string
  className?: string
}) {
  return (
    <div
      role="tablist"
      aria-label={cs.privacy.switcherLabel}
      className={cn('flex items-center gap-1 rounded-xl border border-border bg-s2 p-1', className)}
    >
      <RootTab
        to={scopedRoute(base, 'shared')}
        active={scope === 'shared'}
        icon={<Users className="size-3.5" aria-hidden />}
        label={sharedLabel}
      />
      <RootTab
        to={scopedRoute(base, 'private')}
        active={scope === 'private'}
        // ⚠ The lock is never the only carrier: the label beside it says
        // "Soukromé …" in words. Required for the accessibility pass, and for
        // anyone meeting the icon for the first time.
        icon={<Lock className="size-3.5" aria-hidden />}
        label={privateLabel}
        privateTone
      />
    </div>
  )
}

function RootTab({
  to,
  active,
  icon,
  label,
  privateTone,
}: {
  to: string
  active: boolean
  icon: React.ReactNode
  label: string
  privateTone?: boolean
}) {
  return (
    <Link
      to={to}
      role="tab"
      aria-selected={active}
      className={cn(
        'inline-flex min-h-11 flex-1 items-center justify-center gap-1.5 rounded-lg px-3 text-[13px] font-semibold transition-colors',
        // ⚠ min-h-11 is 44 px — the touch-target floor. This control is the one
        // thing on the page that must never be mis-tapped.
        active
          ? privateTone
            ? 'border border-vis-private bg-vis-private-soft text-fg'
            : 'border border-accent bg-accent-soft text-accent'
          : 'border border-transparent text-muted hover:text-fg',
      )}
    >
      {icon}
      <span className="truncate">{label}</span>
    </Link>
  )
}

/**
 * PrivateMark is the lock treatment for ONE ROW — a tree item, a search hit, a
 * pinned widget row (D183).
 *
 * ⚠ It means exactly one thing: *"only you can see this"*. It is never borrowed
 * for a disabled control, an admin-only route or a locked settlement period.
 * Home renders disabled controls visibly rather than hiding them, and a lock that
 * sometimes means "not yours" and sometimes "not visible to others" destroys both
 * meanings.
 *
 * `withLabel` is the default because the icon alone is not enough — for the
 * accessibility pass, and for the person who has never seen it before. Pass
 * `withLabel={false}` only where the row is ALREADY inside a visibly private
 * container (the private tree's own rows), where repeating the word on every line
 * is noise rather than clarity.
 */
export function PrivateMark({
  withLabel = true,
  label = cs.privacy.privateShort,
  className,
}: {
  withLabel?: boolean
  label?: string
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border border-vis-private bg-vis-private-soft px-1.5 py-0.5 text-[11px] font-semibold text-fg',
        className,
      )}
      // The title is the fallback for the icon-only variant; the visible label
      // carries it otherwise.
      title={withLabel ? undefined : label}
    >
      <Lock className="size-3" aria-hidden />
      {withLabel ? <span>{label}</span> : <span className="sr-only">{label}</span>}
    </span>
  )
}
