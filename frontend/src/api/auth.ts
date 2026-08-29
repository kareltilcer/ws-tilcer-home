// Auth (Mode B) — home hosts its own login and owns its session, so there is no
// JWT in the browser and these three calls are the whole of the client's side of
// it.
//
// This is platform rather than a module: app/auth.tsx is the only place that
// drives it, and every module sits behind the session it establishes. It stays in
// src/api/ beside client.ts for the same reason client.ts does.

import { apiFetch } from './client'
import type {
  SessionUser,
} from './types'

// ---- Auth (Mode B) ----
export const login = (email: string, password: string) =>
  apiFetch<SessionUser>('/api/auth/login', { method: 'POST', body: { email, password }, skipAuthRedirect: true })

export const logout = () => apiFetch<void>('/api/auth/logout', { method: 'POST', skipAuthRedirect: true })

export const getSession = () => apiFetch<SessionUser>('/api/auth/session', { skipAuthRedirect: true })
