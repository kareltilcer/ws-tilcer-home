import { useState, type CSSProperties } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import {
  CalendarClock,
  Files,
  LayoutDashboard,
  LifeBuoy,
  ListTodo,
  Megaphone,
  MessageSquare,
  MoreHorizontal,
  NotebookText,
  ScrollText,
  Sprout,
  Settings,
  Wallet,
  WifiOff,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { Toaster } from 'sonner'
import { cn, initial } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { count, PLURAL } from '@/i18n/plural'
import { useTheme } from '@/theme/theme'
import { useAuth } from '@/app/auth'
import {
  DESKTOP_FOOTER_ROUTES,
  DESKTOP_NAV_ORDER,
  isFullBleedRoute,
  routes,
  type ShellLayout,
} from '@/app/routes'
import { APP_VERSION_LABEL } from '@/platform/version'
import { useLiveSync } from '@/api/ws'
import { useOnline } from '@/platform/pwa/offline'
import { usePushKeepalive } from '@/platform/push/usePush'
import { openFeedback, useFeedbackWidget } from '@/platform/status/feedback'
import { useUnreadTotal } from '@/modules/chat/api/hooks'
import { useSoftKeyboard } from '@/hooks/useSoftKeyboard'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  desc?: string
  adminOnly?: boolean
  /** v10: the chat tab's unread count. Undefined everywhere else. */
  badge?: number
}

// The four daily, thumb-reachable destinations — always shown on the mobile tab bar.
//
// ⚠ v10 IS THE FIRST TIME A MODULE IS DEMOTED TO MAKE ROOM (D260). Four tabs plus
// *Více* is the shape that works at 375 px and a fifth makes six slots, so Chat
// takes a tab and **Okno do budoucnosti moves into the overflow**. Okno is the
// least-daily of the four and the only one whose signal already arrives elsewhere —
// two Nástěnka widgets and four metrics — while chat is the one screen in the app
// carrying an unread count, which is a reason to open something rather than a place
// to end up. Nothing about Okno changes except where its link lives.
const PRIMARY: NavItem[] = [
  { to: routes.nastenka, label: cs.nav.nastenka, icon: LayoutDashboard },
  { to: routes.ukoly, label: cs.nav.ukoly, icon: ListTodo },
  { to: routes.chat, label: cs.nav.chat, icon: MessageSquare },
  { to: routes.poznamky, label: cs.nav.poznamky, icon: NotebookText },
]

// Everything past the four daily tabs. v4 (D49): with Dokumenty a regular member
// has FIVE destinations, so the mobile overflow sheet is no longer admin-only — it
// holds Dokumenty for everyone and the Log for admins. Desktop lists all six.
//
// v5 extends the same overflow: Administrace joins Log for admins only, and
// Nastavení joins it for EVERYONE — it holds the per-device notification panel,
// which is the one notification surface a reader also gets.
//
// v6 extends it once more: Finance sits beside Dokumenty with NO admin gate
// (D84). It is a once-a-month destination, so it does not earn one of the four
// thumb tabs — but every member sees it, unlike Log and Administrace.
//
// v7 extends it once more: Zahrada, also with NO admin gate — but unlike every
// other module it is a doorway to eight sub-pages rather than one screen, so the
// entry lands on Přehled and the module's own tab strip takes over from there.
//
// v8 extends it a final time: Elektřina, ungated, a doorway to four sub-pages
// behind v7's tab strip. It is the only module in the app with NO Nástěnka
// surface whatsoever (D147) and no notification of any kind, so this nav entry
// is quite literally the only way anyone will ever find it — which is why the
// description has to say what the module answers, not what it contains.
//
// v10 puts Okno do budoucnosti at the TOP of it, with its full name (D260) — it is
// a demotion, not a removal, and burying a daily-ish destination at the bottom of a
// sheet beside the admin-only entries would be a third thing nobody decided.
const OVERFLOW: NavItem[] = [
  { to: routes.okno, label: cs.nav.okno, icon: CalendarClock, desc: cs.nav.oknoDesc },
  { to: routes.dokumenty, label: cs.nav.dokumenty, icon: Files, desc: cs.nav.dokumentyDesc },
  { to: routes.finance, label: cs.nav.finance, icon: Wallet, desc: cs.nav.financeDesc },
  { to: routes.zahrada, label: cs.nav.zahrada, icon: Sprout, desc: cs.nav.zahradaDesc },
  { to: routes.elektrina, label: cs.nav.elektrina, icon: Zap, desc: cs.nav.elektrinaDesc },
  { to: routes.log, label: cs.nav.log, icon: ScrollText, desc: cs.nav.logDesc, adminOnly: true },
  { to: routes.administrace, label: cs.admin.title, icon: Megaphone, desc: cs.admin.navDesc, adminOnly: true },
  { to: routes.nastaveni, label: cs.settings.title, icon: Settings, desc: cs.settings.subtitle },
]

// ⚠ AN UNNAMED ENTRY SORTS TO THE END RATHER THAN TO THE FRONT. `indexOf` returns
// -1 for a module nobody added to DESKTOP_NAV_ORDER, and -1 would put it ABOVE Chat
// — a new destination silently jumping the queue is worse than one landing at the
// bottom. `sort` is stable (ES2019), so several unnamed entries keep their table
// order. nav.test.ts fails on the omission either way.
function desktopRank(to: string): number {
  const i = DESKTOP_NAV_ORDER.indexOf(to)
  return i === -1 ? DESKTOP_NAV_ORDER.length : i
}

export function AppShell() {
  // ⚠ ONLY THE TOASTER READS THE THEME HERE. The toggle itself lives in Nastavení
  // (D273) — the shell used to carry a second copy in the side nav and a third in the
  // mobile header, which is three controls for one preference somebody changes twice
  // a year.
  const { theme } = useTheme()
  const { isAdmin, identity, canWrite } = useAuth()
  const [moreOpen, setMoreOpen] = useState(false)
  const location = useLocation()
  useLiveSync()
  // Reconnects a device whose push endpoint the browser rotated. It lives here,
  // not in the settings panel, because the failure it repairs is silent — nobody
  // opens Nastavení to fix a problem they cannot see.
  usePushKeepalive()
  /**
   * ⚠ THE KEYBOARD IS THE SHELL'S TO KNOW ABOUT, NOT CHAT'S. Both things it changes
   * are the shell's own: the thumb-tab bar, which no module renders, and the two
   * tokens the chat viewport is built out of, which reach it down the cascade rather
   * than through a prop. Chat asking the question itself would leave the bar visible
   * over a keyboard nobody can see past.
   */
  const keyboard = useSoftKeyboard()

  /**
   * ⚠ THE WIDGET LOADS INSIDE THE AUTHENTICATED SHELL, not from main.tsx beside
   * crash reporting — and the two placements are a deliberate pair. A crash
   * belongs to the app and has to be caught before anything renders, including on
   * the login screen. A report belongs to a PERSON: it is signed with a display
   * label, and there is nobody to sign it with until somebody is signed in. It
   * also keeps the widget key out of the page a fresh unauthenticated visitor is
   * SERVED — which is the visit that matters, and is all it is: a logout in the
   * same tab leaves the script element in the head while the login screen
   * renders, and the widget key is public by design either way.
   *
   * `false` means the script did not load, or this build has no key — either way
   * the two triggers below are not rendered at all, because a "Nahlásit problém"
   * that does nothing when pressed is worse than no button.
   */
  const feedbackReady = useFeedbackWidget(identity.label)

  // ⚠ THE ROLE LINE IS DERIVED FROM WHAT THE APP GRANTS, not from the roles array it
  // was granted out of. `roles` can carry `*`, several names, or a name this build
  // has never heard of, and a second vocabulary for it would be free to disagree with
  // the one every gate in the app actually reads. This says what you can do here.
  const roleLabel = isAdmin ? cs.app.roleAdmin : canWrite ? cs.app.roleEditor : cs.app.roleReader

  const unread = useUnreadTotal()
  // ⚠ THE BADGE IS ATTACHED HERE RATHER THAN DECLARED IN PRIMARY, because PRIMARY is
  // a module-level constant and the count is a live query. Matching on the route
  // keeps the one dynamic field out of the static table.
  const primaryItems = PRIMARY.map((item) =>
    item.to === routes.chat ? { ...item, badge: unread } : item,
  )
  const overflowItems = OVERFLOW.filter((item) => !item.adminOnly || isAdmin)
  // ⚠ THE SIDE NAV HAS ITS OWN ORDER, WRITTEN DOWN IN ONE PLACE (DESKTOP_NAV_ORDER
  // in app/routes.ts), because it is not the thumb bar's order and never was — the
  // artboards draw Chat at the top of the sidebar and third of five on the phone.
  // Concatenating the two nav tables is what had Chat fourth and Okno fifth; a list
  // that states the order is also a list a test can read back.
  //
  // ⚠ AND THE FOOTER'S ENTRIES ARE TAKEN OUT OF IT RATHER THAN LEFT OUT OF THE TABLES
  // (D273). Nastavení is still an ordinary nav destination — it keeps its row in the
  // phone's sheet — so it stays in OVERFLOW, and the sidebar is the one surface that
  // draws it somewhere else.
  const desktopItems = [...primaryItems, ...overflowItems]
    .filter((item) => !DESKTOP_FOOTER_ROUTES.includes(item.to))
    .sort((a, b) => desktopRank(a.to) - desktopRank(b.to))
  const fullBleed = isFullBleedRoute(location.pathname)
  // The "Více" tab lights up when the open route lives behind it.
  const overflowActive = overflowItems.some(
    (item) => location.pathname === item.to || location.pathname.startsWith(item.to + '/'),
  )

  return (
    /* ⚠ THE TOKENS ARE OVERWRITTEN HERE, ON THE ELEMENT EVERY ROUTE HANGS OFF.
       --chat-viewport becomes what is actually on screen above the keyboard and
       --chat-chrome-bottom drops the thumb bar's 57 px, because the bar is hidden
       for the same span (theme/globals.css carries the arithmetic, ChatPage the one
       formula that reads it). Below 768 that leaves the chat box exactly as tall as
       the strip between the top of the screen and the keyboard: the thread's own
       header stays at the top, the composer sits on the keyboard's edge, and nothing
       is left for the browser to scroll the page for. There is no app header above it
       to allow for since v10.2 (D272). Nothing else in the app reads either token,
       and every width from 768 up ignores both. */
    <div
      className="min-h-full md:flex md:h-screen md:overflow-hidden"
      style={
        keyboard.open
          ? ({
              '--chat-viewport': `${keyboard.viewport}px`,
              '--chat-chrome-bottom': '0px',
            } as CSSProperties)
          : undefined
      }
    >
      <Toaster theme={theme} position="top-center" richColors />
      {/* Desktop side nav */}
      <aside className="hidden md:flex md:w-60 md:flex-col md:border-r md:border-border md:bg-s1">
        <div className="flex items-center gap-2 px-5 py-5">
          <span className="grid h-8 w-8 place-items-center rounded-md bg-accent font-extrabold text-accent-fg">
            h
          </span>
          <span className="text-lg font-extrabold tracking-tight">{cs.app.name}</span>
        </div>
        <nav className="flex flex-1 flex-col gap-1 px-3">
          {desktopItems.map((item) => (
            <SideLink key={item.to} item={item} admin={item.adminOnly} />
          ))}
        </nav>
        {/* The foot of the side nav (design/v10_2, D273). It used to be four stacked
            full-width buttons under a bare email; it is now one row that says WHO you
            are and one ⚙ that goes where you change it, with the version underneath.
            The theme toggle and Odhlásit se moved into Nastavení itself. */}
        <div className="p-3">
          {feedbackReady && (
            <button
              type="button"
              onClick={openFeedback}
              title={cs.feedback.hint}
              className="mb-2 flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border bg-s2 text-sm font-semibold text-fg hover:bg-s3"
            >
              <LifeBuoy size={16} aria-hidden />
              <span>{cs.feedback.open}</span>
            </button>
          )}
          <div className="flex items-center gap-2.5 border-t border-border pt-3">
            {/* Not a photo — home has no user table and no avatars (D230), so the
                mark is the initial, in the same label blue the artboards use.
                ⚠ aria-hidden BECAUSE IT SAYS NOTHING THE NEXT ELEMENT DOES NOT.
                It is decoration under WCAG 1.4.3 — it abbreviates the name printed
                beside it and carries nothing of its own — which is what lets it keep
                the mock's tint at 4.09:1 light / 4.44:1 dark, under the bar that
                would apply if this were the only place the name appeared.
                ⚠ AND aria-hidden IS NOT WHAT EXEMPTS IT FROM THE SWEEP, so do not
                reach for that reasoning again: axe's color-contrast rule matches on
                isVisibleOnScreen and reads aria-hidden text like any other — the
                `admin` tags below are aria-hidden too and failed it. Measured on this
                branch: putting --subtle back on that tag turns the run red with
                `color-contrast [2]` at light/1440. This span escapes as `incomplete`
                ("content is too short to determine if it is actual text"), which is
                axe declining to judge one character, not axe passing it. */}
            <span
              className="grid h-8 w-8 flex-none place-items-center rounded-full bg-l-byt/25 text-[13px] font-bold text-l-byt"
              aria-hidden
            >
              {initial(identity.label)}
            </span>
            {/* ⚠ min-w-0 ON THE BOX THE WIDTH HAS TO STOP AT. A display name is
                arbitrary text in a 240 px column, and a truncating child cannot
                shrink a flex parent whose min-width is still auto — the v10.1 list
                pane clipped for exactly this reason. */}
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12.5px] font-semibold" title={identity.email}>
                {identity.label}
              </span>
              <span className="block font-mono text-[11px] text-muted">{roleLabel}</span>
            </span>
            <NavLink
              to={routes.nastaveni}
              title={cs.settings.title}
              aria-label={cs.settings.title}
              className={({ isActive }) =>
                cn(
                  'grid h-8 w-8 flex-none place-items-center rounded-md border transition-colors',
                  isActive
                    ? 'border-accent bg-accent-soft text-accent'
                    : 'border-border bg-s2 text-muted hover:bg-s3 hover:text-fg',
                )
              }
            >
              <Settings size={16} aria-hidden />
            </NavLink>
          </div>
          <VersionLabel className="px-0.5 pt-2.5" />
        </div>
      </aside>

      {/* Content.

          ⚠ THERE IS NO APP HEADER ON THE PHONE, AND THERE NEVER WAS ONE IN THE DESIGN
          (D272). A 61 px strip carrying the word "home", a theme toggle and a sign-out
          button sat above every screen at 375 px: the name is on the icon that was
          tapped to get here, and the other two are settings, not daily work. The first
          row under the status bar belongs to the screen somebody is standing on —
          which is what the artboards draw. The desktop side nav is unaffected, and
          --chat-chrome-top in theme/globals.css is the arithmetic that had to follow. */}
      <div className="flex min-w-0 flex-1 flex-col">
        <OfflineBanner />
        <main
          className={cn(
            'flex-1 md:min-h-0',
            // Chat sizes itself to the viewport and draws its own edges
            // (isFullBleedRoute), so it takes this box whole and scrolls inside its
            // own panes. `overflow-hidden` rather than `auto`: the panes are exactly
            // as tall as the box, and a sub-pixel rounding that grew a shell
            // scrollbar would put the composer back under the fold.
            fullBleed
              ? 'md:overflow-hidden'
              : 'px-4 py-5 pb-24 md:overflow-y-auto md:px-8 md:py-8 md:pb-8',
          )}
        >
          {/* ⚠ THE DECISION TRAVELS WITH THE OUTLET rather than being made twice.
              Chat's offline screen has to fit whichever box it was handed, and the
              only thing that actually knows which box that is, is the element that
              handed it over. A page re-deriving it from its own pathname would agree
              today and would be free to stop agreeing. */}
          <Outlet context={{ fullBleed } satisfies ShellLayout} />
        </main>
      </div>

      {/* Mobile bottom tab bar: the four daily tabs + the "Více" overflow. Since v4
          every member has something behind "Více" (Dokumenty), so the tab is no
          longer admin-only (D49).

          ⚠ AND IT STANDS DOWN WHILE A KEYBOARD IS UP. It is fixed to the LAYOUT
          viewport, which a keyboard does not shrink, so it was never on screen at
          those moments anyway — it sat behind the keyboard while its 57 px went on
          being subtracted from the chat box above. Hiding it is what lets that 57 px
          go to the thread instead, and it is why --chat-chrome-bottom may be zeroed
          on the root: the two are one decision and must not drift apart. */}
      <nav
        className={cn(
          'fixed inset-x-0 bottom-0 z-10 flex border-t border-border bg-s1 md:hidden',
          keyboard.open && 'hidden',
        )}
      >
        {primaryItems.map((item) => (
          <TabLink key={item.to} item={item} />
        ))}
        {overflowItems.length > 0 && (
          <button
            type="button"
            onClick={() => setMoreOpen(true)}
            aria-haspopup="true"
            aria-expanded={moreOpen}
            className={cn(
              'flex min-h-[56px] flex-1 flex-col items-center justify-center gap-1 text-[11px] font-semibold',
              overflowActive ? 'text-accent' : 'text-muted',
            )}
          >
            <MoreHorizontal size={20} aria-hidden />
            <span className="truncate px-1">{cs.nav.more}</span>
          </button>
        )}
      </nav>

      {/* Overflow sheet (mobile): Dokumenty for everyone, Log for admins. */}
      {overflowItems.length > 0 && moreOpen && (
        <div className="fixed inset-0 z-40 md:hidden" role="presentation" onClick={() => setMoreOpen(false)}>
          <div className="absolute inset-0 bg-black/45" />
          <div
            role="dialog"
            aria-label={cs.nav.moreHeading}
            className="absolute inset-x-0 bottom-0 rounded-t-[20px] border-t border-border-strong bg-s1 p-4 pb-7"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mx-auto mb-3.5 h-1 w-10 rounded-full bg-border-strong" />
            <p className="mb-2.5 font-mono text-[10px] uppercase tracking-wide text-subtle">{cs.nav.moreHeading}</p>
            <div className="space-y-2">
              {overflowItems.map((item) => {
                const Icon = item.icon
                const active = location.pathname === item.to || location.pathname.startsWith(item.to + '/')
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    onClick={() => setMoreOpen(false)}
                    className={cn(
                      'flex min-h-[52px] items-center gap-3 rounded-xl border px-3.5 text-fg',
                      active ? 'border-accent bg-accent-soft' : 'border-border bg-s2',
                    )}
                  >
                    <span className="grid h-[34px] w-[34px] flex-none place-items-center rounded-lg bg-s3 text-muted">
                      <Icon size={16} aria-hidden />
                    </span>
                    <span className="flex-1">
                      <span className="block text-[14.5px] font-bold">{item.label}</span>
                      {item.desc && <span className="block text-[12px] text-subtle">{item.desc}</span>}
                    </span>
                    {item.adminOnly && (
                      <span className="font-mono text-[9.5px] uppercase tracking-wide text-muted" aria-hidden>
                        admin
                      </span>
                    )}
                    {active && (
                      <span className="text-accent" aria-hidden>
                        ✓
                      </span>
                    )}
                  </NavLink>
                )
              })}
              {/* The sheet's one ACTION among its destinations — row-shaped so it
                  is the same 52 px target under a thumb, and last so it never
                  moves when a module is added above it. The sheet closes first:
                  the widget's dialog is its own fixed layer, and leaving a z-40
                  overlay under it would darken the thing it just opened. */}
              {feedbackReady && (
                <button
                  type="button"
                  onClick={() => {
                    setMoreOpen(false)
                    openFeedback()
                  }}
                  className="flex min-h-[52px] w-full items-center gap-3 rounded-xl border border-border bg-s2 px-3.5 text-left text-fg"
                >
                  <span className="grid h-[34px] w-[34px] flex-none place-items-center rounded-lg bg-s3 text-muted">
                    <LifeBuoy size={16} aria-hidden />
                  </span>
                  <span className="flex-1">
                    <span className="block text-[14.5px] font-bold">{cs.feedback.open}</span>
                    <span className="block text-[12px] text-subtle">{cs.feedback.hint}</span>
                  </span>
                </button>
              )}
            </div>
            {/* ⚠ THE HINT THAT USED TO SIT HERE IS GONE, and the version has its place
                (D271). "Čtyři denní moduly zůstávají v dosahu palce. Zbytek je tady."
                described the sheet to somebody who had already opened it; the version
                is the one line on this screen that a member can do something with. */}
            <VersionLabel className="mt-3.5 text-center" />
          </div>
        </div>
      )}
    </div>
  )
}

/** OfflineBanner is the ONE app-wide offline indicator (D71).
 *
 *  Deliberately NEUTRAL, not error-red: being offline is a state, not a failure,
 *  and the app remains fully readable. The second sentence names the two things
 *  that genuinely cannot work rather than leaving the user to discover them. */
function OfflineBanner() {
  const online = useOnline()
  if (online) return null
  return (
    <div
      role="status"
      className="flex flex-wrap items-center gap-x-2.5 gap-y-1 border-b border-border-strong bg-muted/15 px-4 py-1.5 text-[12.5px]"
    >
      <WifiOff size={14} className="text-muted" aria-hidden />
      <span className="font-semibold">{cs.offline.banner}</span>
      <span className="text-muted">{cs.offline.bannerSub}</span>
    </div>
  )
}

function SideLink({ item, admin }: { item: NavItem; admin?: boolean }) {
  const Icon = item.icon
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-semibold transition-colors',
          isActive ? 'bg-s2 text-accent' : 'text-muted hover:bg-s2 hover:text-fg',
        )
      }
    >
      <Icon size={18} aria-hidden />
      <span className="truncate">{item.label}</span>
      {item.badge !== undefined && item.badge > 0 && (
        <span className="ml-auto">
          <UnreadBadge count={item.badge} />
        </span>
      )}
      {/* ⚠ --muted, NOT --subtle, AND THE E2E SWEEP IS WHY. axe has been failing on
          this tag at light/1440 since the day the Log entry got it: --subtle on --s1
          is 4.29:1 in the light theme, under the 4.5:1 AA bar, and 9.5 px is nowhere
          near the large-text exemption. Same swap and same measurement as the
          foreign bubble's author label in theme/globals.css. The pairing is wider
          than these two tags and belongs to the design bundle's tokens. */}
      {admin && (
        <span className="ml-auto font-mono text-[9.5px] uppercase tracking-wide text-muted" aria-hidden>
          admin
        </span>
      )}
    </NavLink>
  )
}

function TabLink({ item }: { item: NavItem }) {
  const Icon = item.icon
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        cn(
          'flex min-h-[56px] flex-1 flex-col items-center justify-center gap-1 text-[11px] font-semibold',
          isActive ? 'text-accent' : 'text-muted',
        )
      }
    >
      <span className="relative">
        <Icon size={20} aria-hidden />
        {item.badge !== undefined && item.badge > 0 && (
          <span className="absolute -right-2.5 -top-1.5">
            <UnreadBadge count={item.badge} />
          </span>
        )}
      </span>
      <span className="truncate px-1">{item.label}</span>
    </NavLink>
  )
}

/**
 * The unread badge (D260).
 *
 * ⚠ IT USES THE ACCENT, NOT THE ATTENTION FAMILY. Unread is not a warning — it is a
 * reason to open something — and borrowing the warning register for it would make
 * every unread message look like a problem.
 *
 * ⚠ AND IT IS MONO AND TABULAR so 3 and 40 occupy the same width: a bar whose tabs
 * shift sideways as messages arrive is a bar whose targets move under a thumb.
 *
 * The accessible name counts, so it declines (PLURAL.unreadMessages) — a fixed
 * phrase beside a number is the one thing D20 rules out.
 */
function UnreadBadge({ count: n }: { count: number }) {
  return (
    <span
      className="grid h-[18px] min-w-[18px] place-items-center rounded-full bg-accent px-1 font-mono text-[10px] font-bold tabular-nums text-accent-fg"
      aria-label={count(n, PLURAL.unreadMessages)}
    >
      {n > 99 ? '99+' : n}
    </span>
  )
}

/**
 * The deployed version, in the two places the artboards put it: the foot of the side
 * nav and the bottom of the phone's "Více" sheet (D271).
 *
 * ⚠ IT IS A LABEL, NOT A CONTROL. Mono, subtle, no border, nothing to press — it is
 * read once, when something has gone wrong and a bug report needs to name a build.
 * It is not an update notice either: the service worker updates the app on its own
 * and D26 has always said nobody is asked to act on a new version.
 *
 * ⚠ THE ACCESSIBLE NAME SAYS WHAT THE STRING IS. `v1.10.2 · a3f9c2e` read out on its
 * own is noise; a `title` alone would not be announced at all, since this is not an
 * interactive element and title is only surfaced on some of those.
 *
 * ⚠ --muted, NOT THE --subtle THE ARTBOARDS SPECIFY, and it is the same measurement
 * theme/globals.css already made for the foreign bubble's author label: --subtle on
 * --s1 is 4.29:1 in the LIGHT theme, under the 4.5:1 AA bar for text this size, and
 * 10.5 px is nowhere near the large-text exemption. --muted is 6.81:1 light and
 * 7.88:1 dark. The label is quiet either way; the hierarchy it loses to the role
 * line above it is a step of grey, and the alternative is a line the person who
 * needs it most cannot read.
 */
function VersionLabel({ className }: { className?: string }) {
  return (
    <p
      title={cs.app.version}
      className={cn('font-mono text-[10.5px] tracking-[0.04em] text-muted', className)}
    >
      <span className="sr-only">{cs.app.version}: </span>
      {APP_VERSION_LABEL}
    </p>
  )
}
