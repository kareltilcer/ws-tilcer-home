// Mode A auth transport against ws-tilcer-auth.
//
// - Refresh: POST {AUTH_BASE}/token/refresh with the shared .tilcer.cz session
//   cookie (credentials:'include') → { access_token, ... } in-body.
// - Login: redirect the browser to the auth SPA at {AUTH_BASE}/login?site=home.
// - Roles come from the site-scoped JWT's `roles` claim.
//
// REAL_AUTH defaults to the build mode: on for production builds, off in dev
// (which uses the offline dev-admin stub mirroring the backend's
// HOME_DEV_AUTH_BYPASS so `npm run dev` / e2e work without reaching
// auth.tilcer.cz). An explicit VITE_REAL_AUTH overrides either way — `true` to
// exercise real auth in dev, `false` to build a production image that runs
// against the backend bypass (the offline docker-compose harness).

const AUTH_BASE = (import.meta.env.VITE_AUTH_BASE_URL as string | undefined) ?? ''
export const SITE = 'home'
const realAuthOverride = import.meta.env.VITE_REAL_AUTH
export const REAL_AUTH: boolean =
  realAuthOverride === 'true'
    ? true
    : realAuthOverride === 'false'
      ? false
      : import.meta.env.PROD

interface TokenResponse {
  access_token: string
  token_type: string
  expires_in: number
  site: string
}

/** refreshToken mints a fresh site-scoped JWT from the session cookie, or null. */
export async function refreshToken(): Promise<string | null> {
  try {
    const res = await fetch(`${AUTH_BASE}/token/refresh`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ site: SITE }),
    })
    if (!res.ok) return null
    const tok = (await res.json()) as TokenResponse
    return tok.access_token
  } catch {
    return null
  }
}

/** redirectToLogin sends the browser to the auth-hosted login for site `home`. */
export function redirectToLogin(): void {
  const url = new URL(`${AUTH_BASE}/login`)
  url.searchParams.set('site', SITE)
  url.searchParams.set('redirect', window.location.href)
  window.location.assign(url.toString())
}

export interface JwtClaims {
  sub?: string
  email?: string
  roles?: string[]
  exp?: number
}

/** decodeJwt reads (does NOT verify) a JWT payload — for surfacing roles/email
 *  in the UI. Authorization is always enforced server-side from the verified token. */
export function decodeJwt(token: string): JwtClaims | null {
  try {
    const payload = token.split('.')[1]
    return JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/'))) as JwtClaims
  } catch {
    return null
  }
}
