import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import * as api from '@/api/auth'
import { setUnauthorizedHandler } from '@/api/client'
import type { UserPublic } from '@/api/types'
import { RedirectingShell } from '@/components/common/RedirectingShell'
import { LoginScreen } from '@/components/auth/LoginScreen'
import { startPersisting, stopPersisting } from '@/platform/pwa/persist'
import { releasePushDevice } from '@/platform/push/usePush'

// Auth identity for the SPA (Mode B). The browser holds NO token — the app learns
// who it is from GET /api/auth/session, and every request is authorized by the
// session cookie. Roles come from home's session (refreshed server-side).
export interface Identity {
  userId: string
  email: string
  label: string
  roles: string[]
}

interface AuthContextValue {
  identity: Identity
  canWrite: boolean
  isAdmin: boolean
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

function hasRole(roles: string[], ...allowed: string[]): boolean {
  return roles.some((r) => r === '*' || allowed.includes(r))
}

function toIdentity(u: UserPublic): Identity {
  return { userId: u.id, email: u.email, label: u.display_name || u.email, roles: u.roles }
}

type State = { status: 'loading' } | { status: 'anon' } | { status: 'auth'; identity: Identity }

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>({ status: 'loading' })
  const qc = useQueryClient()

  const resolve = useCallback(async () => {
    try {
      const { user } = await api.getSession()
      setState({ status: 'auth', identity: toIdentity(user) })
    } catch {
      setState({ status: 'anon' })
    }
  }, [])

  useEffect(() => {
    // Any 401 mid-session (e.g. the session was revoked or a mint failed closed)
    // drops the app to the login screen and clears cached data — including the
    // PERSISTED offline cache, which must never outlive its session (D71).
    setUnauthorizedHandler(() => {
      void stopPersisting(qc)
      setState({ status: 'anon' })
    })
    void resolve()
    return () => setUnauthorizedHandler(null)
  }, [resolve, qc])

  // Persist this user's read cache so the app renders offline. The bucket is
  // namespaced by user id: a household laptop is shared, and the next person to
  // log in must never see the previous one's boards.
  useEffect(() => {
    if (state.status !== 'auth') return
    void startPersisting(qc, state.identity.userId)
  }, [state, qc])

  if (state.status === 'loading') return <RedirectingShell />
  if (state.status === 'anon') {
    return <LoginScreen onSuccess={(user) => setState({ status: 'auth', identity: toIdentity(user) })} />
  }

  const logout = () => {
    // Release this device's push subscription BEFORE the session goes: the
    // endpoint is authenticated, and a subscription that outlives the logout
    // keeps delivering this member's household notifications to a device
    // somebody else may now be using — the same reason the persisted cache is
    // purged here. Best-effort: a failure must never trap anyone in a session.
    void releasePushDevice()
      .catch(() => {})
      .then(() => api.logout())
      .catch(() => {})
      .finally(() => {
        // Purge the persisted bucket too, not just the in-memory cache.
        void stopPersisting(qc)
        setState({ status: 'anon' })
      })
  }

  const identity = state.identity
  const value: AuthContextValue = {
    identity,
    canWrite: hasRole(identity.roles, 'editor', 'admin'),
    isAdmin: hasRole(identity.roles, 'admin'),
    logout,
  }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
