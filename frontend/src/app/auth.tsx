import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { REAL_AUTH, decodeJwt, redirectToLogin } from '@/api/authTransport'
import { ensureRefreshed } from '@/api/client'
import { RedirectingShell } from '@/components/common/RedirectingShell'

// Auth identity for the SPA. Roles come from the site-scoped JWT's `roles` claim
// (PRD §3): reads for any authenticated user, writes for editor|admin, the Log
// browser for admin only ("*" is superuser).
export interface Identity {
  userId: string
  label: string
  roles: string[]
}

// Dev stub: an admin identity so the app is exercisable offline (mirrors the
// backend HOME_DEV_AUTH_BYPASS). Used whenever REAL_AUTH is off (dev / e2e).
const DEV_IDENTITY: Identity = { userId: 'dev-user', label: 'Vývojář', roles: ['admin'] }

interface AuthContextValue {
  identity: Identity
  canWrite: boolean
  isAdmin: boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

function hasRole(roles: string[], ...allowed: string[]): boolean {
  return roles.some((r) => r === '*' || allowed.includes(r))
}

function provide(identity: Identity): AuthContextValue {
  return {
    identity,
    canWrite: hasRole(identity.roles, 'editor', 'admin'),
    isAdmin: hasRole(identity.roles, 'admin'),
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  // Dev / e2e: use the stub identity, render immediately.
  if (!REAL_AUTH) {
    return <AuthContext.Provider value={provide(DEV_IDENTITY)}>{children}</AuthContext.Provider>
  }
  return <RealAuthProvider>{children}</RealAuthProvider>
}

function RealAuthProvider({ children }: { children: ReactNode }) {
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [resolved, setResolved] = useState(false)

  // Mode A boot: refresh from the session cookie; on success decode the JWT for
  // the identity, else hand off to the auth-hosted login.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      const token = await ensureRefreshed()
      if (cancelled) return
      if (!token) {
        redirectToLogin()
        return // keep showing the redirecting shell during the browser handoff
      }
      const claims = decodeJwt(token)
      setIdentity({
        userId: claims?.sub ?? '',
        label: claims?.email ?? '',
        roles: claims?.roles ?? [],
      })
      setResolved(true)
    })()
    return () => {
      cancelled = true
    }
  }, [])

  if (!resolved || !identity) return <RedirectingShell />
  return <AuthContext.Provider value={provide(identity)}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
