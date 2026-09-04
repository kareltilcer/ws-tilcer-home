# home — household management (`home.tilcer.cz`)

A Czech-language household-management SPA over a Go + embedded-SQLite backend,
and the second consumer of the shared `auth` service. A compile-time
**modular monolith**: each module owns its routes, migrations, audit actions, and
dashboard widgets, wired through a central registry (`internal/platform`,
`internal/modules/*`). Auth is **Mode B**: home hosts its own login and owns its
session (no JWT in the browser). **Eight modules**, all built around a central
audit-logging spine:

- **Nástěnka** (`dashboard`) — landing page: a per-user **widget host** that
  renders widgets contributed by the other modules (owns no feature data).
- **Úkoly** (`todo`) — a Trello-style board; contributes the *Právě dělám* widget.
- **Okno do budoucnosti** (`events`) — all-day, optionally recurring reminders;
  contributes the *Připomínky* and *Tento měsíc* widgets.
- **Poznámky** (`notes`, v3) — Markdown notes in a folder tree with slug-path
  URLs, two-scope pinning and inline image upload; contributes *Připnuté poznámky*.
- **Dokumenty** (`documents`, v4) — files in a folder tree, bytes in a dedicated
  R2 bucket, previews and permanent `/d/{id}` links; contributes *Připnuté dokumenty*.
- **Administrace** (`admin`, v5) — admin-only Web Push: broadcasts, audit-key
  trigger rules and scheduled summaries; plus the installable, reads-offline PWA.
- **Finance** (`finance`, v6) — the household's monthly income split into personal
  accounts, the joint operational account and two savings pots, **derived on read**
  by a locked formula; contributes *Rozpočet měsíce*. A clone of the standalone
  `fin` service, which v6 retires. **No new environment variable.**
- **Log** (`logging`) — admin-only audit browser over the logging spine.

Deployed on Coolify as **two apps sharing one origin** (`home.tilcer.cz`),
mirroring ws-tilcer-fin: an API-only Go image and a static Nginx SPA image. The
platform (Coolify/Traefik) routes `home.tilcer.cz/api` and `/ws` to the backend
and everything else to the frontend — there is **no nginx→backend proxy**. The SPA
calls the API host-relative (relative `/api`, websocket to `location.host` — see
`src/api/client.ts` and `ws.ts`), so it stays same-origin with no CORS. Go handles
`/api/**`, `/ws`, `/healthz`, `/readyz`. See `plan.md` for build status and
`handoff/v2/` for the PRD, OpenAPI spec (0.3.0), and engineering handoffs.

## Layout

```
backend/    Go 1.26 + modernc SQLite; cmd/home is the entrypoint; Dockerfile here
frontend/   Vite + React 19 + TS SPA
litestream.yml         Litestream → Cloudflare R2 replica config (prefix `home`)
docker-entrypoint.sh   restore-if-absent then run the app under litestream -exec
frontend/Dockerfile    static SPA image (Nginx; static-only — the platform routes /api + /ws)
frontend/nginx.harness.conf  local-only proxy config, mounted by docker-compose
docker-compose.yml     offline two-service smoke test (backend + Nginx frontend)
```

## Local development

The app runs fully offline with the auth bypass — no Docker, no auth service.

```sh
# Backend (serves the API + websocket on :8080)
cd backend
HOME_DEV_AUTH_BYPASS=true HOME_DB_PATH=./home.db go run ./cmd/home

# Frontend (Vite dev server proxies /api + /ws to :8080)
cd frontend
npm install
npm run dev
```

In dev the frontend uses an offline dev-admin stub (mirrors the backend bypass),
so no login is required. Vite serves the SPA; the Go server does not (its
`HOME_STATIC_DIR` is unset).

### Tests

```sh
cd backend  && go test ./...          # temp-file SQLite; role-gating; atomicity
cd frontend && npm run test           # Vitest (gesture/unit)
cd frontend && npm run test:e2e       # Playwright + axe (a11y, both themes)
```

### Offline container smoke test

Exercises the actual production backend image (multi-stage build, entrypoint)
plus an Nginx-served SPA, without R2. The backend API/websocket is published on
`:7999`; the frontend Nginx serves the SPA on `:7001` and reverse-proxies
`/api` + `/ws` to the backend:

```sh
docker compose up --build     # → http://localhost:7001 as the fake dev admin
```

## Deploy (Coolify)

**Two apps sharing one origin** (`home.tilcer.cz`), exactly like ws-tilcer-fin.
Each is a separate Coolify app built from its own Dockerfile in this repo, and
**both are mapped to `home.tilcer.cz`** — Coolify/Traefik path-routes between them:

- **`home-backend`** — the API-only Go image. Domains: **`home.tilcer.cz/api`** and
  **`home.tilcer.cz/ws`** (path routing; these prefixes go to the backend).
- **`home-frontend`** — the static Nginx SPA image. Domain: **`home.tilcer.cz`**
  (the catch-all; serves the bundle for everything not claimed above).

There is **no nginx→backend proxy** and no shared internal network to wire — the
platform does the split. A longer path prefix wins, so `/api` and `/ws` reach the
backend and all other paths reach the SPA. The SPA calls the API host-relative, so
it is same-origin (no CORS). Coolify health-checks each container directly on its
own port, so `/healthz` + `/readyz` need no public route.

> The backend serves its routes **under** `/api` (see `httpx/router.go`), so the
> prefix must be **preserved**, not stripped — do **not** enable Strip Prefix on
> the backend's path domain, or it receives `/auth/login` instead of
> `/api/auth/login` and 404s.
>
> If the backend app is **not** mapped to `home.tilcer.cz/api` (+`/ws`), those
> requests fall through to the SPA (or 404), and **login fails** — that path
> routing is the whole mechanism.

### Backend app (`home-backend`)

| Setting             | Value                  |
| ------------------- | ---------------------- |
| Build Pack          | Dockerfile             |
| Base Directory      | `/` (repo-root context — the image needs `backend/`, `litestream.yml`, `docker-entrypoint.sh`) |
| Dockerfile Location | `/backend/Dockerfile`  |
| Port                | `7999` (matches `HOME_ADDR`) |
| Domains             | `home.tilcer.cz/api` and `home.tilcer.cz/ws` (path-routed) |
| Health check path   | `/readyz`              |
| Persistent volume   | mount at `/data` (holds the SQLite DB) |

**Runtime env vars** (Coolify — nothing secret in the repo, PRD §9). The API-only
image serves no static assets, so `HOME_STATIC_DIR` stays **unset**.

| Var | Purpose | Value |
| --- | --- | --- |
| `HOME_ENV` | environment; **must be `production`** so the dev bypass is hard-refused | `production` |
| `HOME_ADDR` | TCP listen address | `:7999` (image default) |
| `HOME_DB_PATH` | SQLite file on the persisted volume | `/data/home.db` (image default) |
| `AUTH_BASE_URL` | auth service base (BE→BE `/internal/login` + `/internal/token/mint`; also the target of reset/MFA out-links) | `https://auth.tilcer.cz` |
| `HOME_AUTH_SERVICE_SECRET` | `home` service-client secret (Mode B: authenticates `/internal/login` + `/internal/token/mint`) | *(secret)* |
| `HOME_AUTH_JWT_SECRET` | shared HS256 secret to **verify** the access tokens auth returns (identity + roles live in the JWT claims); **must equal** auth's JWT signing secret | *(secret)* |
| `HOME_AUTH_JWT_ISSUER` | *(optional)* exact `iss` to require on those tokens = **auth's own base URL** (e.g. `https://auth.tilcer.cz`), which is **not** `AUTH_BASE_URL` when that ends in `/api`. Unset = don't check the issuer (signature + audience already bind the token) | *(unset)* |
| `HOME_SITE_KEY` | auth site key | `home` (default) |
| `HOME_ALLOWED_ORIGINS` | CSRF Origin allowlist for cookie-authenticated mutations | `https://*.tilcer.cz` (default) |
| `HOME_SESSION_TTL_DAYS` | home session sliding window (Mode B; 1–3650) | `90` (default) |
| `HOME_ROLE_REFRESH_MINUTES` | how often home re-mints to refresh cached roles (1–1440) | `15` (default) |
| `HOME_WS_REVALIDATE_MINUTES` | how often an already-open websocket re-takes its session decision (1–1440). A socket is authenticated once, at upgrade; logout and a mint failing closed already close it immediately, so this bounds only the revocations nothing announces — an expiring TTL, a row revoked out of band | `5` (default) |
| `HOME_TIMEZONE` | IANA zone for “today”/recurrence | `Europe/Prague` (default) |
| `HOME_DASHBOARD_LOOKBACK_DAYS` | reminder lookback | `30` (default) |
| `HOME_RRULE_MAX_OCCURRENCES` | expansion cap | `500` (default) |
| `HOME_RRULE_MAX_WINDOW_MONTHS` | window-span cap | `24` (default) |
| `HOME_LOG_RETENTION_DAYS` | audit prune threshold; `0` = keep forever | `0` (default) |
| `LITESTREAM_ENABLED` | run under Litestream | `true` |
| `LITESTREAM_R2_ENDPOINT` | R2 S3 endpoint | `https://<account-id>.r2.cloudflarestorage.com` |
| `LITESTREAM_R2_BUCKET` | R2 bucket | *(bucket name)* |
| `LITESTREAM_ACCESS_KEY_ID` | R2 access key | *(secret)* |
| `LITESTREAM_SECRET_ACCESS_KEY` | R2 secret key | *(secret)* |

> ⚠ **The three session windows are range-checked at boot, and the UPPER bounds
> are new in v10.** The caps exist because each value is multiplied into a
> `time.Duration`: a large enough one overflows int64 nanoseconds into a
> *negative* duration that every comparison then reads backwards (re-minting on
> every request, cookies issued with a negative `MaxAge`), and both load silently
> and break login.
>
> `HOME_SESSION_TTL_DAYS` and `HOME_ROLE_REFRESH_MINUTES` shipped with a **floor
> check only**, so a value above the new cap has been legal for their whole life
> and may already be set in Coolify. Those two are therefore **clamped to the cap
> with a loud `CONFIGURATION CORRECTED` warning at startup, not refused** — the
> boot succeeds, the oversized value never reaches the arithmetic, and the
> operator finds it in the logs of a service that is *up*. `Load` aborting would
> instead crash-loop the container on the deploy that lands v10, with the only
> signal a log line inside the restart loop. Values *below* the floor are still
> refused (they always were, so nothing deployed carries one), and
> `HOME_WS_REVALIDATE_MINUTES` is new in v10 and refused at both ends.

**Documents (v4) — the `documents` module stores file BYTES in its own R2 bucket**
(SQLite keeps only metadata). This bucket is **separate from the Litestream DB
replica** and, because Litestream cannot back up blobs, it has its own backup story:
a **daily mirror** into a second bucket (in-process, see below). R2 has **no object
versioning**, so that mirror bucket is the only second copy of the bytes — treat
`HOME_DOCS_R2_BACKUP_BUCKET` as required for real deployments, not optional.

| Var | Purpose | Value |
| --- | --- | --- |
| `HOME_DOCS_R2_BUCKET` | primary documents bucket. **Required when `HOME_ENV=production`** — the server refuses to start without it, because the dev filesystem fallback has no backup coverage | *(bucket name)* |
| `HOME_DOCS_R2_ENDPOINT` | its S3 endpoint | `https://<account-id>.r2.cloudflarestorage.com` |
| `HOME_DOCS_R2_ACCESS_KEY_ID` / `HOME_DOCS_R2_SECRET_ACCESS_KEY` | its credentials | *(secret)* |
| `HOME_DOCS_R2_BACKUP_BUCKET` | mirror target. Empty = mirroring off (reconciliation still runs) | *(bucket name)* |
| `HOME_DOCS_R2_BACKUP_ENDPOINT` / `_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY` | only if the backup lives in a **different** account; otherwise they default to the primary's | *(unset)* |
| `HOME_DOCS_MIRROR_CRON` | mirror + reconciliation interval as a **Go duration** (not a cron expression); `0` disables | `24h` (daily) |
| `HOME_DOCS_ORPHAN_GRACE_HOURS` | how old an object with no row must be before reconciliation deletes it (an upload writes the object before the row, so young orphans are normal) | `24` |
| `HOME_DOCS_ORPHAN_MAX_PERCENT` | blast-radius guard: the most of the bucket one reconciliation pass may delete before it refuses and logs instead. Reconciliation reads "orphan" as "no row claims it", so a database that came up empty or half-restored would otherwise read as *everything is orphaned*. `100` disables the guard | `25` |
| `HOME_DOCS_MAX_UPLOAD_MB` | per-file cap; over it the upload is refused `413` | `50` |
| `HOME_DOCS_ALLOWED_MIME` | optional allowlist checked against the **server-sniffed** type (`image/*` wildcards allowed). Empty = allow all, still sniffed | *(unset)* |
| `HOME_DOCS_GOTENBERG_URL` | the Office→PDF converter service. Empty = Office files stay download-only | `http://gotenberg:3000` |
| `HOME_DOCS_PREVIEW_ENABLED` | master switch for the preview/thumbnail worker | `true` |
| `HOME_DOCS_PREVIEW_TIMEOUT_SEC` | per-job bound; keep it **below** Gotenberg's `--api-timeout` | `60` |
| `HOME_DOCS_PREVIEW_WORKERS` | in-process worker pool size | `2` |
| `HOME_DOCS_PDFTOPPM_PATH` / `HOME_DOCS_CWEBP_PATH` | thumbnail helpers (shipped in the image); a missing binary just skips thumbnails | `pdftoppm` / `cwebp` |
| `HOME_DOCS_THUMB_MAX_PX` | thumbnail longest edge | `480` |
| `HOME_DOCS_IMAGE_MAX_MEGAPIXELS` | largest image the worker will DECODE for a thumbnail. `HOME_DOCS_MAX_UPLOAD_MB` bounds the file, not the pixels — compression ratio is unlimited, and the decode happens in the app process, so an unbounded one is an OOM of the whole backend rather than a failed thumbnail | `50` |
| `HOME_DOCS_PUBLIC_BASE_URL` | absolute base for the permanent `/d/{id}` links. Empty = relative to the app origin | *(unset)* |
| `HOME_DOCS_LOCAL_DIR` | **development only** filesystem store, used when no bucket is set | `/data/blobs` (image default) |

**Notifications (v5) — Web Push + the summary scheduler.** Home is its own push
sender: one service worker on one origin, therefore **one subscription per
device**, and every module sends through the same channel. Members opt in per
device in **Nastavení → Oznámení**; the `admin` module only configures *what* is
sent.

The VAPID keypair is what identifies this server to every browser push service.
**Generate it once and keep it:**

```bash
go run ./cmd/vapidgen
```

Rotating it does not "reset" anything — it **silently invalidates every existing
subscription**, and each household device has to open the settings panel and
subscribe again. Treat it like the auth secret. Leave all three unset and push is
cleanly disabled: nothing is sent, subscribing is refused with a reason, and the
rest of the app is untouched.

| Var | Purpose | Value |
| --- | --- | --- |
| `HOME_VAPID_PUBLIC_KEY` | VAPID public key (base64url). The only half ever served to the browser | *(from `cmd/vapidgen`)* |
| `HOME_VAPID_PRIVATE_KEY` | VAPID private key. **Never leaves the server** | *(secret)* |
| `HOME_VAPID_SUBJECT` | contact for the push service — `mailto:…` or `https://…` | `mailto:karel@tilcer.cz` |
| `HOME_NOTIF_COALESCE_DEFAULT` | default per-rule window collapsing a burst of one rule's matches into one push; `0` = send every event | `60` (seconds) |
| `HOME_NOTIF_DELIVERY_RETENTION_DAYS` | prune the delivery log past this age; `0` = keep forever. Deliveries are **operational, not audit** | `30` |
| `HOME_NOTIF_MAX_FAILDAYS` | prune a subscription that has been failing continuously this long (a device wiped without unsubscribing) | `14` |
| `HOME_PUSH_ENDPOINT_HOSTS` | **extra** push-service hostnames a subscription endpoint may name, on top of the built-in list (Google, Mozilla, Apple, WNS). A subscription's endpoint is what decides where this server POSTs, and every push route is open to every role, so it is allowlisted rather than taken on trust. Bare hostnames, comma-separated — **never URLs**. Matched exactly or as a subdomain | *(unset)* |
| `HOME_SCHED_TICK_SECONDS` | scheduler granularity. Bounded to 1–60: slots are wall-clock **minutes**, so a longer tick steps over them | `60` |
| `HOME_SCHED_CATCHUP_GRACE` | fire a slot missed while the process was down, if it is back within this many minutes; older misses are skipped rather than delivered as stale news | `120` |

A device subscribes with an endpoint URL its browser mints, and that URL is where
this server POSTs for every notification it sends to that device — so it is
checked against a list of known push services rather than trusted. The built-in
list covers Google (Chrome/Edge/Opera/Brave), Mozilla, Apple and legacy WNS,
which is every browser that can subscribe today. If a household device is ever
refused with *"neznámá push služba: …"*, the hostname in that message goes in
`HOME_PUSH_ENDPOINT_HOSTS` — the boot line reports the effective list as
`push_hosts=builtin` or `builtin+[…]`.

Schedules are evaluated in `HOME_TIMEZONE` and are DST-correct: an 08:00 summary
stays at 08:00 local across both boundaries. A day-of-month of 29–31 **clamps to
the month's last day** in short months, matching the events module's short-month
rule — "the 31st" fires on 28/29 February rather than skipping the month.

> **Do not** set `HOME_DEV_AUTH_BYPASS` in production — with `HOME_ENV=production`
> the server refuses to start if it is enabled (fake auth in prod is a security
> hole). `/readyz` also reports `insecure_auth` whenever the bypass is active.

**Crash reporting to `status.tilcer.cz`.** These four are the backend half; the
browser half is a separate key in the frontend's build args, and both are
explained together under
[Crash reporting and feedback](#crash-reporting-and-feedback-statustilcercz)
below. Not `HOME_`-prefixed on purpose: they are the fleet-wide names every
`ws-tilcer-*` service reads. Leave all four unset and reporting is cleanly off —
the boot line says `statusreport: DISABLED` and nothing else changes.

| Var | Purpose | Value |
| --- | --- | --- |
| `STATUS_INGEST_URL` | the site's ingest endpoint, e.g. `https://status.tilcer.cz/api/ingest/home`. Set it and `STATUS_INGEST_KEY` together or neither — exactly one is refused at boot, because a half-configured reporter drops every event in silence | *(unset — reporting off)* |
| `STATUS_INGEST_KEY` | that site's ingest key (`ik_…`). Keep it out of the repo and the logs like any other secret — but ⚠ it stops being one the moment the SAME key is baked into the SPA, which is the default wiring below | *(secret; unset — reporting off)* |
| `STATUS_ENVIRONMENT` | environment tag on every event. Set it only to say something `HOME_ENV` cannot, e.g. `staging` — and ⚠ **never on its own**: set the frontend's `VITE_STATUS_ENVIRONMENT` to the same string in the same deploy, or the two halves file one release under two names (see below) | defaults to `HOME_ENV` mapped onto status's own vocabulary — `production`→`prod`, `development`→`dev`, which is what the SPA sends too, so one deployment reaches the board under **one** name |
| `STATUS_RELEASE` | free-form release tag, e.g. `home@2026.36.1`. Same rule: set it with `VITE_STATUS_RELEASE` or with neither | *(unset)* |

### Documents converter (`home-gotenberg`, v4)

Office→PDF previews run in a **Gotenberg** sidecar rather than a LibreOffice binary
inside the backend image — a deliberate deviation from `handoff/v4/HANDOFF-6-documents.md`
§16, agreed with Karel: it keeps the backend image at ~100 MB instead of ~1 GB and
isolates the converter from the app process.

| Setting             | Value                        |
| ------------------- | ---------------------------- |
| Image               | `gotenberg/gotenberg:8`      |
| Command             | `gotenberg --api-timeout=90s --libreoffice-restart-after=10` |
| Port                | `3000` — **internal only, no public domain** |
| Persistent volume   | none (stateless)             |

The backend reaches it at `HOME_DOCS_GOTENBERG_URL`. If it is down or unset, Office
uploads succeed and become **download-only** (`preview_status="failed"`/`"none"`) —
a preview is never allowed to fail an upload.

### Frontend app (`home-frontend`)

Static-only image — **no runtime env vars**.

| Setting             | Value                  |
| ------------------- | ---------------------- |
| Build Pack          | Dockerfile             |
| Base Directory      | `/frontend`            |
| Dockerfile Location | `/frontend/Dockerfile` |
| Port                | `80`                   |
| Domain              | `home.tilcer.cz` (catch-all) |

**Build args** (baked into the SPA at image build time)

| Arg                  | Purpose                     | Value |
| -------------------- | --------------------------- | ----- |
| `VITE_AUTH_BASE_URL` | the "Zapomněli jste heslo?" / MFA out-links, and nothing else — Mode B carries no browser token | `https://auth.tilcer.cz` |
| `VITE_STATUS_INGEST_URL` | browser crash reporting. Unset ⇒ the SPA installs no error listeners | e.g. `https://status.tilcer.cz/api/ingest/home`; *(unset — reporting off)* |
| `VITE_STATUS_INGEST_KEY` | the site's ingest key (`ik_…`). **Public by design**, like a Sentry DSN — see below | *(unset — reporting off)* |
| `VITE_STATUS_WIDGET_KEY` | the site's widget key (`wk_…`), from **site detail → User feedback** | *(unset ⇒ no widget and no "Nahlásit problém" trigger)* |
| `VITE_STATUS_SITE` | the site id in status | `home` |
| `VITE_STATUS_ENVIRONMENT` | environment tag. Leave it unset **unless the backend's `STATUS_ENVIRONMENT` was set** — then set it to the same string. Unset on both sides, the two defaults already agree, because the backend maps `HOME_ENV` onto these same two words | `prod` in a production build, `dev` under `npm run dev` |
| `VITE_STATUS_RELEASE` | free-form release tag, e.g. `home@2026.36.1`. Same rule: set it with `STATUS_RELEASE` or with neither | *(unset)* |
| `VITE_STATUS_WIDGET_URL` | override the widget bundle (a staging status). Pin a **major**: `/widget/v1.js` | `https://status.tilcer.cz/widget/v1.js` |

⚠ **Both status keys are baked, so rotating either one needs a frontend
REBUILD**, not a variable change. That is inherent to a static Nginx image — the
runtime sees no environment — and is the same trade `VITE_AUTH_BASE_URL` makes.

⚠ **The browser ingest key is public and that is the design.** Anyone who loads
the page can read it; all it can do is POST crashes for one site into a
rate-limited, size-capped endpoint — exactly a Sentry DSN.

⚠ **And status issues ONE ingest key per site**, so by default this is the *same*
value as the backend's `STATUS_INGEST_KEY` — which means the backend's copy is
public too, whatever it is called in the table above. That is the trade, and it
is fine: the key's whole authority is "POST a crash for site `home`". If it ever
needs to stop being fine, the fix is a **second status site** for the browser
half with its own key and its own board — not a second key on this one, which
status does not offer. The **widget** key (`wk_…`) is a genuinely different key
on a different endpoint: rotating it never touches crash reporting.

### Crash reporting and feedback (`status.tilcer.cz`)

Two separate things, two separate keys, both optional. The contract lives in
`ws-tilcer-status/docs/integration.md` (crashes) and `docs/widget.md` (feedback).

**What reports.** On the backend, every `logger.Error(...)` — a mirror pass that
cannot list its bucket, a recovered panic, a failed session revoke — plus two
`fatal` events written by hand: a panic that unwinds `main`, and a boot that
returns an error (the crash-loop a failed migration produces). Errors reach the
board because the codebase already reserves `Error` for a fault that should not
happen and logs everything soft at `Warn`; `internal/platform/statusreport` wraps
the slog handler in `main` and inherits that curation whole. In the browser,
uncaught errors and unhandled rejections — React 19 routes an uncaught render
error through `window.reportError`, so the white screen arrives too.

**What it costs when status is down: nothing.** Every path fails safe. An ingest
error — 401, 404, 413, 429, a dead socket — is dropped in silence on both sides,
`Capture` never blocks a request path, and the client carries the server's own
60/min-burst-120 limit so an error storm cannot become a socket per log line.
The price is that a wrong key looks exactly like a quiet week, which is why the
boot line says which state reporting is in, once.

**Wiring it up.**

1. In the status dashboard, **Add site** with the id `home` and copy the ingest
   key (shown once). Put it in the backend app's `STATUS_INGEST_KEY` and set
   `STATUS_INGEST_URL`.
2. The **same** key and URL go into the frontend build args as
   `VITE_STATUS_INGEST_URL` + `VITE_STATUS_INGEST_KEY`, which puts both halves of
   `home` on one board. ⚠ It is public from that moment — see the warning above;
   a separate status **site** is the only way to keep a private backend key.
3. **site detail → User feedback → Feedback enabled** issues the widget key
   (`wk_…`, also shown once). It goes in `VITE_STATUS_WIDGET_KEY`.
4. Confirm `https://home.tilcer.cz` is inside status's `STATUS_ALLOWED_ORIGINS`
   (default `https://*.tilcer.cz`, so it already is). ⚠ An origin outside that
   list gets no `Access-Control-Allow-Origin`, the browser blocks the POST, and —
   because the client is fail-safe — **nothing is logged anywhere at all**.
5. Confirm `https://home.tilcer.cz` is in the **attachment bucket's own CORS
   policy** (`widget.md` §8 — a Cloudflare dashboard setting on status's R2
   bucket, not a variable in either repo). ⚠ The widget PUTs a screenshot
   **straight to R2**, not through status, so this is a second allow-list and
   `STATUS_ALLOWED_ORIGINS` above does not cover it. Miss it and the report
   arrives without its file. That much is *not* silent, in either direction: the
   widget names the file it could not upload (*"Tohle se nahrát nepovedlo…"*) and
   the claim call still reaches status, which marks the attachment `missing` on
   the board — `widget.md` §10 sends you from that word straight back to this
   step. What it costs is the screenshot itself, once per reporter, until someone
   reads the board.
6. *(Optional)* Set the site's **Monitor URL**. ⚠ Not `/readyz`: only `/api` and
   `/ws` are path-routed to the backend, so `home.tilcer.cz/readyz` reaches the
   SPA's catch-all and returns the shell with a 200 — a probe that can never
   fail. `https://home.tilcer.cz` monitors the frontend honestly; monitoring the
   backend would mean routing a probe path to it first.
7. Trigger a test error and confirm it appears on the board.

⚠ **The `environment` and `release` tags are set in PAIRS or not at all.** Left
unset everywhere, the two halves agree by construction: the backend maps
`HOME_ENV` onto status's `prod`/`dev` and the SPA's default is the same two
words. Setting one side alone breaks that, and it is the one misconfiguration
neither half can see — `STATUS_ENVIRONMENT` is a backend runtime variable and
`VITE_STATUS_ENVIRONMENT` is a frontend build arg, so there is no moment at which
one process holds both. `STATUS_ENVIRONMENT=staging` without
`VITE_STATUS_ENVIRONMENT=staging` puts one deployment of one release on one board
under two environments — `staging` for everything the Go process reports,
`prod` for everything the browser does. The same goes for `STATUS_RELEASE` and
`VITE_STATUS_RELEASE`. Change them together, in the same deploy, or change
neither.

⚠ **Leave "Send console output" OFF for this site.** It is off by default and
must stay off. home's privacy model makes a member's private notes unreadable by
anyone, admins included — and a console line carrying a note title into status
would be read by Karel's admin session, which is a side door with a different
lock. That is a dashboard setting; nothing in this repository can enforce it.

⚠ **For the same reason a crash report names the module, not the page.** home's
URLs carry slugged titles — `/poznamky/soukrome/<a private note's title>`,
`/dokumenty/<a filename>` — so the browser reporter sends the origin and the
first path segment only, never `location.href`. The **widget** does send the
whole URL (it is in `widget.md` §3 and home cannot change it), but it shows the
reporter everything before they press send: a member deciding about their own
page is not the same as a crash deciding for them.

⚠ **The feedback trigger is home's own, not the widget's floating launcher**
(`data-launcher="none"`). The launcher sits 16 px from a corner and does not
account for a host app's bottom navigation, and home has a 56 px thumb-tab bar
under every width below 768. The cost of supplying our own: the widget's launcher
renders only when feedback is actually enabled server-side, while ours is shown
on the strength of the script having *loaded* — so if feedback is switched off in
the dashboard after a build shipped with a key, the trigger opens nothing.
Rotating the widget key is the fix, and it needs a frontend rebuild either way.

⚠ **A document CSP would need three directives** and home sends none today, so
this is a tripwire rather than a task. If one is ever added:
`script-src https://status.tilcer.cz`, `style-src 'unsafe-inline'`, and — on ONE
`connect-src` line, because a repeated directive is ignored — both
`https://status.tilcer.cz` and the R2 account origin the widget PUTs attachments
to. `require-trusted-types-for 'script'` is not supported by the widget at all.
(The `Content-Security-Policy: sandbox` home already sends on served images, PDFs
and chat content is a per-response sandbox on a served file and is unrelated.)

### Prerequisites before the first deploy (Karel)

1. **Auth registration (Mode B)** — register the `home` site in auth (roles
   `admin`/`editor`/`reader`), provision a `home` service client bound to site
   `home`, and put its secret in Coolify as `HOME_AUTH_SERVICE_SECRET`. In Mode B
   this client authenticates **`/internal/login`** and **`/internal/token/mint`**
   (not just introspect) — confirm the auth service exposes both. Both endpoints
   return a **signed access token** whose claims carry the identity + roles, so
   also set `HOME_AUTH_JWT_SECRET` in Coolify to the **same HS256 secret the auth
   service signs with** — home verifies the token and reads roles from it; a
   mismatch makes every login fail closed with empty roles (no admin, no write).
   Create the household member accounts in auth (no self-signup on home).
2. **R2 (database)** — create the bucket and an access key; set the four
   `LITESTREAM_*` vars above. Backups land under the `home/` prefix.
3. **R2 (documents, v4)** — create **two** more buckets: a primary for document
   bytes and a backup for the mirror. (R2 has **no object versioning** — the mirror
   bucket is the only delete safety net, so configure it rather than skipping it.)
   Put the credentials in `HOME_DOCS_R2_*` and the mirror target in
   `HOME_DOCS_R2_BACKUP_BUCKET`. Neither bucket may be public — content is served
   only through the session-gated backend.
4. **Deploy the Gotenberg service** (above) and point `HOME_DOCS_GOTENBERG_URL` at
   it; keep it internal.
5. **Verify the Litestream image tag** in `backend/Dockerfile`
   (`litestream/litestream:0.3.13`) resolves and its config format matches; bump
   if needed.
6. **status.tilcer.cz** — see the section above. Until it is done, nothing is
   broken and nothing is reported: the boot line reads
   `statusreport: DISABLED`, the SPA installs no error listeners, and no
   "Nahlásit problém" trigger is rendered.

### Fresh-build restore test (must actually run, not assume — HANDOFF F7)

> Already verified locally against a MinIO R2 stand-in (2026-07-22): fresh-volume
> restore returns the data with no double-seed. Re-run against the real R2 once
> deployed.

On boot the entrypoint runs `litestream restore -if-db-not-exists
-if-replica-exists` **before** serving: it restores only when the DB is absent
locally *and* a backup exists, so a first-ever deploy (empty R2) starts fresh
instead of crashing, and a rebuilt/wiped volume is repopulated from R2.

Once deployed and some data exists:

1. Confirm objects appear under the `home/` prefix in R2.
2. Delete/recreate the `/data` volume (or redeploy on a clean volume).
3. Redeploy; the prior data returns and the default board is **not** re-seeded
   (seed runs only when `boards` is empty).
4. `/healthz` and `/readyz` are green.

**Documents (v4) also restore**, but by a different route: the metadata rides
Litestream like every other table, and the file bytes are simply still in R2. So
after the restore, previously-uploaded documents open normally, and previews are
**not** re-derived — `preview_key`/`thumbnail_key` already point at surviving
objects. Check the daily mirror line in the logs
(`documents: blob mirror + reconciliation pass`): `dangling_rows` should be 0. A
non-zero count means a row's object is missing and wants investigating; the pass
never deletes a row on its own.

When live, update `REGISTRY.md` (in Nextcloud) status to **live**.
