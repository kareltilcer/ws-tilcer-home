import { cs } from '@/i18n/cs'
import { Spinner } from '@/components/ui/ui'

// The signed-out / redirecting state (design gap #3): a calm, on-brand screen
// shown while we resolve the session or hand off to auth.tilcer.cz.
export function RedirectingShell() {
  return (
    <div className="grid min-h-full place-items-center bg-bg p-8">
      <div className="flex flex-col items-center gap-4 text-center">
        <span className="grid h-12 w-12 place-items-center rounded-lg bg-accent text-xl font-extrabold text-accent-fg">
          h
        </span>
        <Spinner />
        <p className="text-sm text-muted">{cs.app.redirecting}</p>
      </div>
    </div>
  )
}
