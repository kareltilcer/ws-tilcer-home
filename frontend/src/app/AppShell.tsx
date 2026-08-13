import { useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import {
  CalendarClock,
  Files,
  LayoutDashboard,
  ListTodo,
  LogOut,
  MoreHorizontal,
  Moon,
  NotebookText,
  ScrollText,
  Sun,
  type LucideIcon,
} from 'lucide-react'
import { Toaster } from 'sonner'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { useTheme } from '@/theme/theme'
import { useAuth } from '@/app/auth'
import { routes } from '@/app/routes'
import { useLiveSync } from '@/api/ws'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  desc?: string
  adminOnly?: boolean
}

// The four daily, thumb-reachable destinations — always shown on the mobile tab bar.
const PRIMARY: NavItem[] = [
  { to: routes.nastenka, label: cs.nav.nastenka, icon: LayoutDashboard },
  { to: routes.ukoly, label: cs.nav.ukoly, icon: ListTodo },
  { to: routes.okno, label: cs.nav.oknoShort, icon: CalendarClock },
  { to: routes.poznamky, label: cs.nav.poznamky, icon: NotebookText },
]

// Everything past the four daily tabs. v4 (D49): with Dokumenty a regular member
// has FIVE destinations, so the mobile overflow sheet is no longer admin-only — it
// holds Dokumenty for everyone and the Log for admins. Desktop lists all six.
const OVERFLOW: NavItem[] = [
  { to: routes.dokumenty, label: cs.nav.dokumenty, icon: Files, desc: cs.nav.dokumentyDesc },
  { to: routes.log, label: cs.nav.log, icon: ScrollText, desc: cs.nav.logDesc, adminOnly: true },
]

export function AppShell() {
  const { theme, toggle } = useTheme()
  const { isAdmin, identity, logout } = useAuth()
  const [moreOpen, setMoreOpen] = useState(false)
  const location = useLocation()
  useLiveSync()

  const overflowItems = OVERFLOW.filter((item) => !item.adminOnly || isAdmin)
  const desktopItems = [...PRIMARY, ...overflowItems]
  // The "Více" tab lights up when the open route lives behind it.
  const overflowActive = overflowItems.some(
    (item) => location.pathname === item.to || location.pathname.startsWith(item.to + '/'),
  )

  return (
    <div className="min-h-full md:flex md:h-screen md:overflow-hidden">
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
        <div className="space-y-2 p-3">
          <div className="truncate px-1 text-[12px] text-muted" title={identity.email}>
            {identity.label}
          </div>
          <ThemeToggle theme={theme} onToggle={toggle} />
          <button
            type="button"
            onClick={logout}
            className="flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border bg-s2 text-sm font-semibold text-fg hover:bg-s3"
          >
            <LogOut size={16} aria-hidden />
            <span>{cs.app.signOut}</span>
          </button>
        </div>
      </aside>

      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-border px-4 py-3 md:hidden">
          <span className="text-base font-extrabold tracking-tight">{cs.app.name}</span>
          <div className="flex items-center gap-2">
            <ThemeToggle theme={theme} onToggle={toggle} compact />
            <button
              type="button"
              onClick={logout}
              aria-label={cs.app.signOut}
              title={cs.app.signOut}
              className="grid h-9 w-9 place-items-center rounded-md border border-border bg-s2 text-fg hover:bg-s3"
            >
              <LogOut size={16} aria-hidden />
            </button>
          </div>
        </header>
        <main className="flex-1 px-4 py-5 pb-24 md:min-h-0 md:overflow-y-auto md:px-8 md:py-8 md:pb-8">
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom tab bar: the four daily tabs + the "Více" overflow. Since v4
          every member has something behind "Více" (Dokumenty), so the tab is no
          longer admin-only (D49). */}
      <nav className="fixed inset-x-0 bottom-0 z-10 flex border-t border-border bg-s1 md:hidden">
        {PRIMARY.map((item) => (
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
                      <span className="font-mono text-[9.5px] uppercase tracking-wide text-subtle" aria-hidden>
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
            </div>
            <p className="mt-3 text-center text-[11.5px] text-subtle text-pretty">{cs.nav.moreHint}</p>
          </div>
        </div>
      )}
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
      {admin && (
        <span className="ml-auto font-mono text-[9.5px] uppercase tracking-wide text-subtle" aria-hidden>
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
      <Icon size={20} aria-hidden />
      <span className="truncate px-1">{item.label}</span>
    </NavLink>
  )
}

function ThemeToggle({
  theme,
  onToggle,
  compact = false,
}: {
  theme: 'dark' | 'light'
  onToggle: () => void
  compact?: boolean
}) {
  const Icon = theme === 'dark' ? Sun : Moon
  const label = theme === 'dark' ? 'Přepnout na světlý motiv' : 'Přepnout na tmavý motiv'
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={label}
      title={label}
      className={cn(
        'flex items-center gap-2 rounded-md border border-border bg-s2 text-sm font-semibold text-fg hover:bg-s3',
        compact ? 'h-9 w-9 justify-center' : 'h-9 w-full justify-center px-3',
      )}
    >
      <Icon size={16} aria-hidden />
      {!compact && <span>{theme === 'dark' ? 'Světlý' : 'Tmavý'}</span>}
    </button>
  )
}
