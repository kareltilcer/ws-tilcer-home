// The API fetch wrapper (Mode B). Same-origin: paths are relative ("/api/..."),
// so no CORS; in dev Vite proxies /api and /ws to Go.
//
// Authorization is the home SESSION COOKIE — sent automatically with
// credentials:'include'. There is NO bearer token in JS. State-changing requests
// carry the double-submit CSRF token read from the (JS-readable) `csrf` cookie.
// On 401 we hand off to the login screen (home refreshes roles server-side, so
// there is no client-side token refresh).

import { clientId } from './clientId'

let onUnauthorized: (() => void) | null = null

/** setUnauthorizedHandler registers what to do on a 401 (the auth provider routes
 *  to the login screen). */
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  onUnauthorized = fn
}

/** reportUnauthorized runs the same handoff a 401 does, for the one place that
 *  learns the session ended without issuing a request: the websocket, which the
 *  server closes with a policy code when it revokes (see api/ws.ts). Without it
 *  an idle tab whose account was disabled sits on the app shell indefinitely,
 *  reconnecting into an upgrade that will 401 every time. */
export function reportUnauthorized(): void {
  onUnauthorized?.()
}

/** csrfToken reads the double-submit token from the `csrf` cookie (not HttpOnly). */
function csrfToken(): string {
  const m = document.cookie.match(/(?:^|;\s*)csrf=([^;]+)/)
  return m ? decodeURIComponent(m[1]) : ''
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

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
  // skipAuthRedirect leaves a 401 to the caller instead of routing to login —
  // used by the auth endpoints themselves (login bad-creds, the session probe).
  skipAuthRedirect?: boolean
}

export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = {}
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (!SAFE_METHODS.has(method)) {
    const t = csrfToken()
    if (t) headers['X-CSRF-Token'] = t
    // Identifies this tab so the resulting websocket push echoes back with our
    // id as `origin` — we then skip toasting our own change (see api/ws.ts).
    headers['X-Client-Id'] = clientId
  }

  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  })

  if (res.status === 401 && !opts.skipAuthRedirect) {
    onUnauthorized?.()
    throw new ApiError(401, 'unauthorized')
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

/** apiUpload posts a multipart body (a document upload) and reports progress.
 *
 *  It uses XMLHttpRequest rather than fetch for one reason: fetch has no upload
 *  progress event, and the upload queue shows a per-file progress bar. Everything
 *  else matches apiFetch — the session cookie rides along, the CSRF token and the
 *  client id are attached, and a 401 routes to login.
 *
 *  Content-Type is deliberately NOT set: the browser must write the multipart
 *  boundary itself. */
export async function apiUpload<T>(
  path: string,
  form: FormData,
  opts: { onProgress?: (fraction: number) => void; signal?: AbortSignal } = {},
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', path, true)
    xhr.withCredentials = true
    const t = csrfToken()
    if (t) xhr.setRequestHeader('X-CSRF-Token', t)
    xhr.setRequestHeader('X-Client-Id', clientId)

    if (opts.onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) opts.onProgress?.(e.loaded / e.total)
      }
    }
    opts.signal?.addEventListener('abort', () => xhr.abort(), { once: true })

    xhr.onload = () => {
      if (xhr.status === 401) {
        onUnauthorized?.()
        reject(new ApiError(401, 'unauthorized'))
        return
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve((xhr.responseText ? JSON.parse(xhr.responseText) : undefined) as T)
        } catch {
          reject(new ApiError(xhr.status, 'bad_response'))
        }
        return
      }
      let code = 'error'
      let detail: string | undefined
      try {
        const parsed = JSON.parse(xhr.responseText) as { error?: string; detail?: string }
        code = parsed.error ?? code
        detail = parsed.detail
      } catch {
        // non-JSON error body
      }
      reject(new ApiError(xhr.status, code, detail))
    }
    xhr.onerror = () => reject(new ApiError(0, 'network_error'))
    xhr.onabort = () => reject(new ApiError(0, 'aborted'))
    xhr.send(form)
  })
}
