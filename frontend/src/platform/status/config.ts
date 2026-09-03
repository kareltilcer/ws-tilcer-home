// Configuration for the two things this app sends to status.tilcer.cz: crash
// reports, and the feedback widget the household writes to Karel through.
//
// Both are read from VITE_* build args, baked in at image build time, because
// the SPA is served by a static Nginx image that can see no runtime environment
// at all (frontend/Dockerfile). That is the same mechanism VITE_AUTH_BASE_URL
// already uses; the cost is that rotating either key needs a frontend rebuild,
// which is stated in the README beside the args.
//
// ⚠ THE INGEST KEY IS PUBLIC AND THAT IS THE DESIGN, like a Sentry DSN. Anyone
// who loads the page can read it, and all it can do is POST crashes for one
// site into a rate-limited, size-capped endpoint. status issues ONE ingest key
// per site, so this is normally the SAME value the backend app holds in
// STATUS_INGEST_KEY — baking it here is what makes that copy public too, and a
// backend key that has to stay private needs a second status SITE rather than a
// second key. The WIDGET key below is a genuinely different key on a different
// endpoint: rotating it never touches crash reporting.
//
// Everything here returns null rather than a half-filled object when a variable
// is missing, so an unconfigured build — every local `npm run dev` — is off
// rather than pointed at nothing.

const env = import.meta.env as Record<string, string | undefined>

function trimmed(value: string | undefined): string {
  return (value ?? '').trim()
}

/** The environment tag on every event and report. */
export const STATUS_ENVIRONMENT: string =
  trimmed(env.VITE_STATUS_ENVIRONMENT) || (import.meta.env.DEV ? 'dev' : 'prod')

/** The release tag, e.g. "home@2026.36.1". Optional; empty simply omits it. */
export const STATUS_RELEASE: string = trimmed(env.VITE_STATUS_RELEASE)

/** The site id as it exists in the status dashboard. */
export const STATUS_SITE: string = trimmed(env.VITE_STATUS_SITE) || 'home'

export interface CrashConfig {
  /** The full per-site ingest endpoint, e.g. https://status.tilcer.cz/api/ingest/home */
  url: string
  /** The site's public browser ingest key (ik_…). */
  key: string
  environment: string
  release: string
}

/** crashConfig returns the browser reporter's configuration, or null when this
 *  build was not given one. */
export function crashConfig(): CrashConfig | null {
  const url = trimmed(env.VITE_STATUS_INGEST_URL)
  const key = trimmed(env.VITE_STATUS_INGEST_KEY)
  if (!url || !key) return null
  return { url, key, environment: STATUS_ENVIRONMENT, release: STATUS_RELEASE }
}

export interface WidgetConfig {
  /** The widget bundle's URL. Pin a MAJOR (v1.js): it is served immutable for a
   *  year, and a breaking change ships as v2.js so this embed keeps working. */
  src: string
  site: string
  /** The site's widget key (wk_…), issued once when feedback is enabled. */
  key: string
  release: string
}

/** widgetConfig returns the feedback widget's configuration, or null when this
 *  build has no widget key — in which case nothing is loaded and no trigger is
 *  rendered, rather than a button that does nothing when pressed. */
export function widgetConfig(): WidgetConfig | null {
  const key = trimmed(env.VITE_STATUS_WIDGET_KEY)
  if (!key) return null
  // The widget calls back to the origin its own src came from, so a staging copy
  // talks to staging and the API host is never named twice.
  const src = trimmed(env.VITE_STATUS_WIDGET_URL) || 'https://status.tilcer.cz/widget/v1.js'
  return { src, site: STATUS_SITE, key, release: STATUS_RELEASE }
}
