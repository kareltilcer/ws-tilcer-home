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

/** The environment tag on every event and report.
 *
 *  ⚠ IT MUST BE THE SAME STRING THE BACKEND SENDS. The Go half defaults to
 *  HOME_ENV mapped onto status's prod/dev vocabulary (config.StatusEnv), which is
 *  what this default reproduces — but a deployment that sets STATUS_ENVIRONMENT
 *  and not VITE_STATUS_ENVIRONMENT files one release on one board under two
 *  names, and nothing in either half can detect that: one is a runtime variable
 *  and the other is a build arg, so no process ever holds both. The README states
 *  the pairing rule beside both tables; this is the other end of it. */
const STATUS_ENVIRONMENT: string =
  trimmed(env.VITE_STATUS_ENVIRONMENT) || (import.meta.env.DEV ? 'dev' : 'prod')

/** The release tag, e.g. "home@2026.36.1". Optional; empty simply omits it — and
 *  paired with STATUS_RELEASE for the reason above. */
const STATUS_RELEASE: string = trimmed(env.VITE_STATUS_RELEASE)

/** The site id as it exists in the status dashboard. */
const STATUS_SITE: string = trimmed(env.VITE_STATUS_SITE) || 'home'

export interface CrashConfig {
  /** The full per-site ingest endpoint, e.g. https://status.tilcer.cz/api/ingest/home */
  url: string
  /** The site's public browser ingest key (ik_…). */
  key: string
  environment: string
  release: string
}

/** crashConfig returns the browser reporter's configuration, or null when this
 *  build was not given one — or was given an endpoint it cannot use.
 *
 *  ⚠ THE URL IS CHECKED FOR THE SAME REASON THE BACKEND CHECKS ITS OWN. A value
 *  that is not an absolute http(s) URL is not a broken request here, which is
 *  what makes it worth refusing: `fetch("status.tilcer.cz/api/ingest/home")`
 *  resolves RELATIVE to the page, so every report is POSTed to
 *  home.tilcer.cz/status.tilcer.cz/… — where nginx's SPA fallback answers 200
 *  with index.html. The reporter sees a success, nothing reaches the board, and
 *  there is no failing request anywhere to notice. config.status() refuses this
 *  shape at boot on the Go side with the same reasoning; a build arg deserves the
 *  same guard, and returning null leaves reporting cleanly OFF rather than
 *  pointed at ourselves. */
export function crashConfig(): CrashConfig | null {
  const url = trimmed(env.VITE_STATUS_INGEST_URL)
  const key = trimmed(env.VITE_STATUS_INGEST_KEY)
  if (!url || !key || !isAbsoluteHttpURL(url)) return null
  return { url, key, environment: STATUS_ENVIRONMENT, release: STATUS_RELEASE }
}

/** isAbsoluteHttpURL reports whether value parses as an absolute http(s) URL.
 *  `new URL` with no base throws on a relative one, which is exactly the
 *  distinction that matters — and the protocol check is what keeps a `file:` or
 *  `javascript:` paste from being treated as an endpoint. */
function isAbsoluteHttpURL(value: string): boolean {
  try {
    const { protocol } = new URL(value)
    return protocol === 'https:' || protocol === 'http:'
  } catch {
    return false
  }
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
 *  build has no widget key — or was given a bundle URL it cannot use — in which
 *  case nothing is loaded and no trigger is rendered, rather than a button that
 *  does nothing when pressed.
 *
 *  ⚠ THE BUNDLE URL IS CHECKED LIKE THE INGEST URL, and for the same reason: a
 *  value that is not an absolute http(s) URL is not a broken request here.
 *  `src="status.tilcer.cz/widget/v1.js"` resolves RELATIVE to the page, so the
 *  SPA asks home.tilcer.cz for it and nginx's fallback answers 200 with
 *  index.html — HTML executed as a script, which fails without ever reaching
 *  status. One rule for both build args that name an endpoint. */
export function widgetConfig(): WidgetConfig | null {
  const key = trimmed(env.VITE_STATUS_WIDGET_KEY)
  if (!key) return null
  // The widget calls back to the origin its own src came from, so a staging copy
  // talks to staging and the API host is never named twice.
  const src = trimmed(env.VITE_STATUS_WIDGET_URL) || 'https://status.tilcer.cz/widget/v1.js'
  if (!isAbsoluteHttpURL(src)) return null
  return { src, site: STATUS_SITE, key, release: STATUS_RELEASE }
}
