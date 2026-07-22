# home — household management (`home.tilcer.cz`)

A Czech-language household-management SPA over a Go + embedded-SQLite backend,
and the second consumer of the shared `auth` service. Four modules, all built
around a central audit-logging spine:

- **Nástěnka** (`dashboard`) — landing page: active reminders + every card in a
  `now` column across all boards.
- **Úkoly** (`todo`) — a Trello-style board.
- **Okno do budoucnosti** (`events`) — all-day, optionally recurring reminders.
- **Log** (`logging`) — admin-only audit browser over the logging spine.

One Coolify container serves everything on a single origin: Go handles
`/api/**`, `/ws`, `/healthz`, `/readyz`, and serves the built SPA on every other
path (`index.html` fallback for client-side routes). See `plan.md` for build
status and `handoff/v1/` for the PRD, OpenAPI spec, and engineering handoffs.

## Layout

```
backend/    Go 1.26 + modernc SQLite; cmd/home is the entrypoint; Dockerfile here
frontend/   Vite + React 19 + TS SPA
litestream.yml         Litestream → Cloudflare R2 replica config (prefix `home`)
docker-entrypoint.sh   restore-if-absent then run the app under litestream -exec
frontend/Dockerfile    static SPA image (Nginx; proxies /api + /ws to the backend)
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

Single app on `home.tilcer.cz`, built from the Dockerfile.

**Build settings**

| Setting             | Value                  |
| ------------------- | ---------------------- |
| Build Pack          | Dockerfile             |
| Base Directory      | `/` (repo root context — the image needs both `frontend/` and `backend/`) |
| Dockerfile Location | `/backend/Dockerfile`  |
| Port                | `8080`                 |
| Health check path   | `/readyz`              |
| Persistent volume   | mount at `/data` (holds the SQLite DB) |

**Frontend build args** (baked into the SPA at image build time)

| Arg                  | Value                       |
| -------------------- | --------------------------- |
| `VITE_AUTH_BASE_URL` | `https://auth.tilcer.cz`    |
| `VITE_REAL_AUTH`     | *unset* (a production build turns real auth on automatically) |

**Runtime env vars** (Coolify — nothing secret in the repo, PRD §9)

| Var | Purpose | Value |
| --- | --- | --- |
| `HOME_ENV` | environment; **must be `production`** so the dev bypass is hard-refused | `production` |
| `HOME_DB_PATH` | SQLite file on the persisted volume | `/data/home.db` (image default) |
| `HOME_STATIC_DIR` | built SPA directory | `/srv/web` (image default) |
| `AUTH_BASE_URL` | auth service base (server-side introspect/refresh) | `https://auth.tilcer.cz` |
| `HOME_AUTH_SERVICE_SECRET` | `home` service-client secret for `/introspect` | *(secret)* |
| `HOME_SITE_KEY` | auth site key | `home` (default) |
| `HOME_ALLOWED_ORIGINS` | CORS allowlist for the cross-subdomain refresh flow | `https://home.tilcer.cz` |
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

> **Do not** set `HOME_DEV_AUTH_BYPASS` in production — with `HOME_ENV=production`
> the server refuses to start if it is enabled (fake auth in prod is a security
> hole). `/readyz` also reports `insecure_auth` whenever the bypass is active.

### Prerequisites before the first deploy (Karel)

1. **Auth registration** — register the `home` site in auth (roles
   `admin`/`editor`/`reader`) and provision a `home` service client bound to
   site `home`; put its secret in Coolify as `HOME_AUTH_SERVICE_SECRET`.
2. **R2** — create the bucket and an access key; set the four `LITESTREAM_*`
   vars above. Backups land under the `home/` prefix.
3. **Verify the Litestream image tag** in `backend/Dockerfile`
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

When live, update `REGISTRY.md` (in Nextcloud) status to **live**.
