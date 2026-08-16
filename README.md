# home — household management (`home.tilcer.cz`)

A Czech-language household-management SPA over a Go + embedded-SQLite backend,
and the second consumer of the shared `auth` service. **v2** — a compile-time
**modular monolith**: each module owns its routes, migrations, audit actions, and
dashboard widgets, wired through a central registry (`internal/platform`,
`internal/modules/*`). Auth is **Mode B**: home hosts its own login and owns its
session (no JWT in the browser). Four modules, all built around a central
audit-logging spine:

- **Nástěnka** (`dashboard`) — landing page: a per-user **widget host** that
  renders widgets contributed by the other modules (owns no feature data).
- **Úkoly** (`todo`) — a Trello-style board; contributes the *Právě dělám* widget.
- **Okno do budoucnosti** (`events`) — all-day, optionally recurring reminders;
  contributes the *Připomínky* and *Tento měsíc* widgets.
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
| `HOME_SESSION_TTL_DAYS` | home session sliding window (Mode B) | `90` (default) |
| `HOME_ROLE_REFRESH_MINUTES` | how often home re-mints to refresh cached roles | `15` (default) |
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

| Arg                  | Value                       |
| -------------------- | --------------------------- |
| `VITE_AUTH_BASE_URL` | `https://auth.tilcer.cz` (only for the "Zapomněli jste heslo?" / MFA out-links; Mode B carries no browser token) |

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
