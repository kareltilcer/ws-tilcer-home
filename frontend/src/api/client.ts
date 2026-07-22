// The API fetch wrapper. Same-origin: paths are relative ("/api/..."), so no
// CORS. In dev, Vite proxies /api and /ws to the Go backend.
//
// Mode A (production): attach the in-memory bearer; on 401 do a single shared
// /token/refresh → retry once → redirect to login on failure. In dev the backend
// bypass authenticates every request, so no token is attached and 401 never
// occurs (REAL_AUTH is false).

import { REAL_AUTH, redirectToLogin, refreshToken } from './authTransport'

let accessToken: string | null = null
let refreshing: Promise<string | null> | null = null

/** setAccessToken stores the in-memory bearer (never persisted — XSS). */
export function setAccessToken(token: string | null): void {
  accessToken = token
}

/** getAccessToken returns the current bearer (used by the websocket client). */
export function getAccessToken(): string | null {
  return accessToken
}

/** ensureRefreshed coalesces concurrent 401s into one in-flight refresh. */
export async function ensureRefreshed(): Promise<string | null> {
  if (!refreshing) {
    refreshing = refreshToken().then((t) => {
      accessToken = t
      refreshing = null
      return t
    })
  }
  return refreshing
}

export class ApiError extends Error {
  status: number
  code: string
  detail?: string
  constructor(status: number, code: string, detail?: string) {
    super(detail ? `${code}: ${detail}` : code)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.detail = detail
  }
}

export interface RequestOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal
}

function rawFetch(path: string, opts: RequestOptions, token: string | null): Promise<Response> {
  const headers: Record<string, string> = {}
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers['Authorization'] = `Bearer ${token}`
  return fetch(path, {
    method: opts.method ?? 'GET',
    credentials: 'include',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  })
}

export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  let res = await rawFetch(path, opts, accessToken)

  if (res.status === 401 && REAL_AUTH) {
    const token = await ensureRefreshed()
    if (!token) {
      redirectToLogin()
      throw new ApiError(401, 'unauthorized')
    }
    res = await rawFetch(path, opts, token)
    if (res.status === 401) {
      redirectToLogin()
      throw new ApiError(401, 'unauthorized')
    }
  }

  if (!res.ok) {
    let code = 'error'
    let detail: string | undefined
    try {
      const parsed = (await res.json()) as { error?: string; detail?: string }
      code = parsed.error ?? code
      detail = parsed.detail
    } catch {
      // non-JSON error body
    }
    throw new ApiError(res.status, code, detail)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}
