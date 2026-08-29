import { useState, type FormEvent } from 'react'
import { Loader2 } from 'lucide-react'
import * as api from '@/api/auth'
import { ApiError } from '@/api/client'
import { AUTH_BASE_URL } from '@/api/authTransport'
import type { UserPublic } from '@/api/types'
import { cs } from '@/i18n/cs'

type LoginError = 'creds' | 'disabled' | 'mfa' | 'server' | null

// LoginScreen is home's own login (Mode B, D23): email + password posted to home,
// which verifies against auth BE→BE. No token handling in JS. No signup/reset/MFA
// UI — those stay auth-hosted (home links out).
export function LoginScreen({ onSuccess }: { onSuccess: (user: UserPublic) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<LoginError>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      const { user } = await api.login(email, password)
      onSuccess(user)
    } catch (err) {
      setSubmitting(false)
      if (err instanceof ApiError) {
        if (err.status === 401) setError('creds')
        else if (err.status === 403) setError('disabled')
        else if (err.status === 409) setError('mfa')
        else setError('server')
      } else {
        setError('server')
      }
    }
  }

  return (
    <div className="grid min-h-full place-items-center bg-bg p-6">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center gap-2">
          <span className="grid h-9 w-9 place-items-center rounded-md bg-accent font-extrabold text-accent-fg">h</span>
          <span className="text-xl font-extrabold tracking-tight">{cs.app.name}</span>
        </div>

        <form onSubmit={submit} className="rounded-xl border border-border bg-s1 p-5 shadow-sm">
          <h1 className="mb-1 text-lg font-bold">{cs.login.heading}</h1>
          <p className="mb-4 text-[13px] text-muted">{cs.login.subtitle}</p>

          {error && <ErrorAlert kind={error} />}

          <label className="mb-3 block">
            <span className="mb-1 block text-[13px] font-semibold text-subtle">{cs.login.email}</span>
            <input
              type="email"
              autoComplete="username"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="h-10 w-full rounded-md border border-border bg-s2 px-3 text-sm text-fg outline-none focus:border-accent"
            />
          </label>
          <label className="mb-4 block">
            <span className="mb-1 block text-[13px] font-semibold text-subtle">{cs.login.password}</span>
            <input
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="h-10 w-full rounded-md border border-border bg-s2 px-3 text-sm text-fg outline-none focus:border-accent"
            />
          </label>

          <button
            type="submit"
            disabled={submitting}
            className="flex h-10 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-bold text-accent-fg disabled:opacity-60"
          >
            {submitting && <Loader2 size={16} className="animate-spin" aria-hidden />}
            {cs.login.submit}
          </button>

          <div className="mt-4 space-y-1 text-center text-[12.5px] text-muted">
            <a href={AUTH_BASE_URL} className="text-accent hover:underline">
              {cs.login.forgot}
            </a>
            <p>{cs.login.noAccount}</p>
          </div>
        </form>
      </div>
    </div>
  )
}

function ErrorAlert({ kind }: { kind: Exclude<LoginError, null> }) {
  if (kind === 'mfa') {
    return (
      <div role="alert" className="mb-4 rounded-md border border-warn/40 bg-warn/10 p-3 text-[13px] text-fg">
        <p className="mb-1 font-semibold">{cs.login.mfaTitle}</p>
        <a href={AUTH_BASE_URL} className="text-accent hover:underline">
          {cs.login.mfaLink}
        </a>
      </div>
    )
  }
  const msg = kind === 'creds' ? cs.login.errCreds : kind === 'disabled' ? cs.login.errDisabled : cs.login.errServer
  return (
    <div role="alert" className="mb-4 rounded-md border border-danger/40 bg-danger/10 p-3 text-[13px] text-danger">
      {msg}
    </div>
  )
}
