import type { ReactNode } from 'react'
import { Check, ShieldAlert } from 'lucide-react'
import { cs } from '@/i18n/cs'

/** ScreenHeader is the standard page title block. */
export function ScreenHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <header className="mb-6">
      <h1 className="text-2xl font-extrabold tracking-tight">{title}</h1>
      {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
    </header>
  )
}

/** Placeholder marks a screen that is scaffolded but not yet built. */
export function Placeholder({ note }: { note: string }) {
  return (
    <div className="rounded-lg border border-dashed border-border bg-s1 p-8 text-center text-sm text-muted">
      {note}
    </div>
  )
}

/** EmptySuccess is the calm "nothing needs you" success state (Nástěnka). */
export function EmptySuccess({ title, body }: { title: string; body: string }) {
  return (
    <div className="grid min-h-[340px] place-items-center text-center">
      <div className="max-w-sm">
        <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-lg bg-good/15 text-good">
          <Check size={26} aria-hidden />
        </div>
        <div className="mb-1.5 text-lg font-bold">{title}</div>
        <p className="text-sm text-muted text-pretty">{body}</p>
      </div>
    </div>
  )
}

/** AccessDenied is the guarded-route refusal (admin-only areas). */
export function AccessDenied() {
  return (
    <div className="grid min-h-[340px] place-items-center text-center">
      <div className="max-w-sm">
        <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-lg bg-danger/15 text-danger">
          <ShieldAlert size={26} aria-hidden />
        </div>
        <div className="mb-1.5 text-lg font-bold">{cs.common.accessDenied}</div>
        <p className="text-sm text-muted text-pretty">{cs.common.accessDeniedDetail}</p>
      </div>
    </div>
  )
}

/** Card is a basic surfaced container used across placeholder screens. */
export function Card({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-border bg-s1 p-6 shadow-[var(--shadow)]">{children}</div>
}
