import { NavLink, Outlet } from 'react-router-dom'
import {
  CalendarClock,
  LayoutDashboard,
  ListTodo,
  Moon,
  ScrollText,
  Sun,
  type LucideIcon,
} from 'lucide-react'
import { Toaster } from 'sonner'
import { cn } from '@/lib/utils'
import { cs } from '@/i18n/cs'
import { useTheme } from '@/theme/theme'
import { useAuth } from '@/app/auth'
import { useLiveSync } from '@/api/ws'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  adminOnly?: boolean
}

const NAV: NavItem[] = [
  { to: '/', label: cs.nav.nastenka, icon: LayoutDashboard },
  { to: '/ukoly', label: cs.nav.ukoly, icon: ListTodo },
  { to: '/okno', label: cs.nav.oknoShort, icon: CalendarClock },
  { to: '/log', label: cs.nav.log, icon: ScrollText, adminOnly: true },
]

export function AppShell() {
  const { theme, toggle } = useTheme()
  const { isAdmin } = useAuth()
  const items = NAV.filter((i) => !i.adminOnly || isAdmin)
  useLiveSync()

  return (
    <div className="min-h-full md:flex">
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
          {items.map((item) => (
            <SideLink key={item.to} item={item} />
          ))}
        </nav>
        <div className="p-3">
          <ThemeToggle theme={theme} onToggle={toggle} />
        </div>
      </aside>

      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-border px-4 py-3 md:hidden">
          <span className="text-base font-extrabold tracking-tight">{cs.app.name}</span>
          <ThemeToggle theme={theme} onToggle={toggle} compact />
        </header>
        <main className="flex-1 px-4 py-5 pb-24 md:px-8 md:py-8 md:pb-8">
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom tab bar */}
      <nav className="fixed inset-x-0 bottom-0 z-10 flex border-t border-border bg-s1 md:hidden">
        {items.map((item) => (
          <TabLink key={item.to} item={item} />
        ))}
      </nav>
    </div>
  )
}

function SideLink({ item }: { item: NavItem }) {
  const Icon = item.icon
  return (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-semibold transition-colors',
          // Active = accent TEXT on a surface (AA-safe both themes, same as the
          // mobile tab bar). White-on-accent and text-fg-on-accent-soft both
          // failed AA, so we colour the text, not the background.
          isActive ? 'bg-s2 text-accent' : 'text-muted hover:bg-s2 hover:text-fg',
        )
      }
    >
      <Icon size={18} aria-hidden />
      <span className="truncate">{item.label}</span>
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
