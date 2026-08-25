import { Link } from 'react-router-dom'
import { Lock } from 'lucide-react'
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
 * ⚠ **It is v7's tab strip — underline, full-width, its own row — not a
 * segmented pill group.** The two roots ARE the page's top-level navigation, so
 * they get the same shape Administrace and Zahrada use for theirs (design/v9
 * `Home.dc.html`, the `Kořen` tablist). The bordered `accent-soft` pill is that
 * strip's *second* level; borrowing it here made a page-level switch look like a
 * filter chip parked in the toolbar, and put the two roots at the far right of a
 * row that wraps.
 *
 * ⚠ THE STRIP DRAWS ITS OWN RULE (`border-b`) and callers must not supply one.
 * The underline IS the selected-state signal, so it needs that rule to sit on; a
 * caller that forgot the class left a 2 px stub floating in space, and four
 * hand-assembled copies had already drifted apart. `px-*` from a caller widens
 * the border box, so the rule still runs full-bleed while the tabs stay inset.
 *
 * ⚠ THE ACTIVE PRIVATE TAB IS TINTED, not merely underlined. `--vis-private`
 * aliases `--info`, which is a near-grey: at oklch(0.470 0.040 256) light /
 * oklch(0.760 0.045 256) dark it sits at the same lightness and almost the same
 * hue as `--muted`, the colour of an *inactive* tab's own label. A 2 px rule in
 * it therefore read as decoration, so the private root — the one where being
 * wrong is unrecoverable — was the root whose tab did not look selected. The
 * soft tint carries the state; the rule keeps the private hue. The shared tab
 * stays a bare accent underline, so the household tree is never the one that
 * looks guarded.
 *
 * It is a pair of LINKS rather than a toggle because the scope lives in the URL
 * (see lib/scope.ts): there is no state to hold, the back button works, and a
 * pasted link lands where the sender was standing.
 *
 * ⚠ And because they are links, this is a `nav` with `aria-current="page"` —
 * NOT `role="tablist"`/`role="tab"`. The tab roles overrode the anchors' own,
 * so a screen reader announced "tab, 1 of 2", promising arrow-key navigation
 * that does not exist here and a tabpanel that is nowhere in the DOM. These
 * "tabs" replace the whole page. design/v9 `Home.dc.html` marks the current one
 * with `aria-current` for the same reason.
 */
export function RootSwitcher({
  scope,
  base,
  sharedLabel,
  privateLabel,
  mobile,
  className,
}: {
  scope: Scope
  /** routes.poznamky | routes.dokumenty */
  base: string
  sharedLabel: string
  privateLabel: string
  /**
   * The mobile strip: 44 px tabs instead of 38 px. This control is the one thing
   * on the page that must never be mis-tapped, and 375 px one-handed is the
   * viewport the whole switcher exists to solve. Everything else about the two
   * variants — the rule, the alignment — lives in here, so a caller only ever
   * supplies its own spacing.
   */
  mobile?: boolean
  className?: string
}) {
  return (
    <nav
      aria-label={cs.privacy.switcherLabel}
      // No wrap: at 375 px "Poznámky · Soukromé poznámky" shrinks and truncates
      // rather than reflowing into two ragged rows. The scroll is the last
      // resort behind that (see RootTab's min-w-0) — never the first, because a
      // root that has scrolled off the edge is a root with no way in, which is
      // the bug this switcher was added to fix.
      className={cn('flex gap-0.5 overflow-x-auto border-b border-border', className)}
      style={{ scrollSnapType: 'x proximity' }}
    >
      <RootTab
        to={scopedRoute(base, 'shared')}
        active={scope === 'shared'}
        label={sharedLabel}
        mobile={mobile}
      />
      <RootTab
        to={scopedRoute(base, 'private')}
        active={scope === 'private'}
        // ⚠ The lock is never the only carrier: the label beside it says
        // "Soukromé …" in words. Required for the accessibility pass, and for
        // anyone meeting the icon for the first time.
        icon={<Lock className="size-3.5 flex-none" aria-hidden />}
        label={privateLabel}
        privateTone
        mobile={mobile}
      />
    </nav>
  )
}

function RootTab({
  to,
  active,
  icon,
  label,
  privateTone,
  mobile,
}: {
  to: string
  active: boolean
  icon?: React.ReactNode
  label: string
  privateTone?: boolean
  mobile?: boolean
}) {
  return (
    <Link
      to={to}
      aria-current={active ? 'page' : undefined}
      className={cn(
        // ⚠ min-w-0 + truncate, NOT flex-none: both roots must stay on screen and
        // tappable at any width. Intrinsic widths let "Soukromé dokumenty" scroll
        // off the right edge at 150 % text zoom, and the strip offers no scroll
        // affordance — a member in the shared root could not see the way across.
        'inline-flex min-w-0 scroll-ms-1 items-center gap-1.5 border-b-2 px-3.5 text-[13.5px] transition-colors',
        // ⚠ The ring is drawn INSIDE the tab. The global :focus-visible outline
        // is offset outwards, and overflow-x on the strip forces overflow-y to
        // clip too, which sliced the top and bottom off it.
        'focus-visible:-outline-offset-2',
        mobile ? 'min-h-11' : 'min-h-[38px]',
        active
          ? cn(
              'font-bold text-fg',
              privateTone ? 'rounded-t-md border-vis-private bg-vis-private-soft' : 'border-accent',
            )
          : 'border-transparent font-semibold text-muted hover:text-fg',
      )}
      style={{ scrollSnapAlign: 'start' }}
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
