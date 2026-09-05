# syntax=docker/dockerfile:1
#
# Static SPA image for the `home` frontend, mirroring ws-tilcer-fin: build the
# Vite bundle, then serve it with Nginx. home's client is hardwired same-origin —
# relative /api paths, websocket to location.host (see frontend/src/api/client.ts
# and ws.ts). Which Nginx config is baked is chosen by the NGINX_CONF build arg in
# stage 2 (static-only for the two-apps model; proxying for the compose harness).
#
# Build context is ./frontend (see docker-compose.yml).

# ── Stage 1: build the SPA ─────────────────────────────────────────────
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY . .
# Vite bakes VITE_* at build time; the Nginx runtime image can't see env. The
# offline harness forces the SPA off real auth to match the backend's dev bypass;
# for a real build leave these unset (VITE_REAL_AUTH on, real auth base URL set).
ARG VITE_REAL_AUTH=""
ARG VITE_AUTH_BASE_URL=""
# Crash reporting + the feedback widget (status.tilcer.cz). All optional: with
# none of them set the SPA installs no error listeners, loads no widget and
# renders no "Nahlásit problém" trigger — which is what every local build gets.
#
# ⚠ THE INGEST KEY HERE IS PUBLIC BY DESIGN, like a Sentry DSN: it is baked into
# a bundle anyone can read, and all it can do is POST crashes for one site into a
# rate-limited, size-capped endpoint.
#
# ⚠ AND STATUS ISSUES ONE INGEST KEY PER SITE, so this is normally the SAME value
# as the backend app's STATUS_INGEST_KEY — which is therefore public as well, and
# should not be reasoned about as a secret once it has been baked here. Keeping a
# private backend key means a second status SITE for the browser half, not a
# second key on this one. (The widget key is a different key on a different
# endpoint; rotating it never touches crash reporting.)
#
# ⚠ AND BOTH KEYS ARE BAKED, so rotating either one needs a frontend REBUILD, not
# a variable change. That is inherent to a static Nginx image — the runtime sees
# no environment at all — and is the same trade VITE_AUTH_BASE_URL already makes.
ARG VITE_STATUS_INGEST_URL=""
ARG VITE_STATUS_INGEST_KEY=""
ARG VITE_STATUS_WIDGET_KEY=""
ARG VITE_STATUS_WIDGET_URL=""
ARG VITE_STATUS_SITE=""
ARG VITE_STATUS_ENVIRONMENT=""
ARG VITE_STATUS_RELEASE=""
# The commit half of the version label in the app's chrome (D271/D274). The VERSION
# half is read out of package.json, which is already in this image two COPY lines up;
# this is the part only the build knows, and it is what tells two deploys of one
# release apart in a bug report.
#
# ⚠ IT DEFAULTS TO COOLIFY'S OWN SOURCE_COMMIT, so a normal deploy needs nothing set
# — but Coolify EXCLUDES that variable from the build by default, and the toggle is
# "Include Source Commit in Build" in the application's advanced settings. With it
# off the label simply reads "v1.10.2" with no commit: version.ts prints the version
# alone rather than printing an unexpanded variable.
#
# ⚠ AND THE REASON COOLIFY GIVES FOR EXCLUDING IT DOES NOT APPLY TO THIS FILE. The
# concern is that a value changing every commit invalidates every layer below it —
# true, but the layer below it here is `RUN npm run build`, and `RUN npm ci` sits
# ABOVE `COPY . .`, keyed only on package.json + package-lock.json — which this arg
# cannot reach, so it never re-runs the install. (⚠ A RELEASE commit does re-run it,
# since D274 moved the version into package.json. That is D274's own cost and the
# toggle adds nothing on top of it.) The only builds that pay for the toggle are
# commits that touch NOTHING under frontend/; on any commit that touches something,
# `COPY . .` has already invalidated the build layer and the arg costs nothing on top.
#
# ⚠⚠ THE NEXT LINE HAS NO `=""` AND THAT IS THE WHOLE POINT — DO NOT "TIDY" IT.
# `ARG SOURCE_COMMIT=""` here is a re-declaration WITH a default, and a default written
# in this file BEATS THE `ARG` LINE COOLIFY SPLICES IN: the label shipped commit-less
# even with the toggle on. Bare, the name inherits whatever the platform supplied and
# falls back to an empty string when nothing did. `frontend/src/platform/version.test.ts`
# asserts this line is bare, and asserts its POSITION too — because a default is not the
# only edit that breaks it — since prose is not a reader.
#
# ⚠ COOLIFY SUPPLIES BUILD VARIABLES TWO WAYS AT ONCE, AND THE TWO ARE NOT EQUALLY
# STRONG. That inequality is the whole bug, so it is worth the lines:
#
#   1. A KEY-ONLY `--build-arg 'KEY'` (`generateDockerBuildArgs` emits the key and
#      nothing else). Docker takes the value from the environment of the process running
#      the build, which Coolify fills by sourcing `/artifacts/build-time.env`. When the
#      key IS in that file this path beats EVERY default in this file, `=""` included.
#      When it is NOT, the flag carries nothing at all and the Dockerfile's own default
#      decides — measured: `--build-arg SOURCE_COMMIT` with the name unset in the
#      environment is indistinguishable from passing nothing.
#   2. A SPLICED `ARG KEY=<value>` LINE, inserted after every FROM (older Coolify used a
#      hardcoded line index 1 — coollabsio/coolify#7118 — which lands BEFORE the first
#      FROM whenever line 0 is a comment, as it is here). This path is the WEAK one: a
#      later `ARG KEY=""` in the same stage RESETS it, and an in-stage `ARG KEY=""`
#      SHADOWS a global one. Both were built against this file and read back out of
#      `dist/assets/*.js`; bare inherits under both, `=""` yields empty under both.
#
# ⚠ AND THAT IS THE ASYMMETRY THE VITE_* ARGS ABOVE ARE LIVING ON. `SOURCE_COMMIT` does
# go into the same `env_args` collection they do (`generate_env_variables`), so it does
# get a `--build-arg` — but a key-only flag is only as good as the environment behind it.
# User build-time variables are written into `build-time.env` unconditionally, so path 1
# always fires for them and their `=""` never gets the chance to lose. `SOURCE_COMMIT`
# reaches that file only through `generate_coolify_env_variables(forBuildTime: true)`,
# which is gated on the toggle AND skipped outright when the application defines its own
# variable named `SOURCE_COMMIT` — while the splice carries it with no such guard. So it
# can arrive by path 2 ALONE, which `ARG SOURCE_COMMIT=""` beats. That is the failure.
# The other args keep their `=""` because they are not what broke, not because they are
# safe by construction: put an empty `SOURCE_COMMIT` or `VITE_APP_COMMIT` in the Coolify
# UI and path 1 fires with an empty value, which beats bare and the splice alike.
ARG SOURCE_COMMIT
ARG VITE_APP_COMMIT=$SOURCE_COMMIT
ENV VITE_REAL_AUTH=$VITE_REAL_AUTH \
    VITE_AUTH_BASE_URL=$VITE_AUTH_BASE_URL \
    VITE_STATUS_INGEST_URL=$VITE_STATUS_INGEST_URL \
    VITE_STATUS_INGEST_KEY=$VITE_STATUS_INGEST_KEY \
    VITE_STATUS_WIDGET_KEY=$VITE_STATUS_WIDGET_KEY \
    VITE_STATUS_WIDGET_URL=$VITE_STATUS_WIDGET_URL \
    VITE_STATUS_SITE=$VITE_STATUS_SITE \
    VITE_STATUS_ENVIRONMENT=$VITE_STATUS_ENVIRONMENT \
    VITE_STATUS_RELEASE=$VITE_STATUS_RELEASE \
    VITE_APP_COMMIT=$VITE_APP_COMMIT
RUN npm run build            # emits /app/dist

# ── Stage 2: serve with Nginx ──────────────────────────────────────────
# Which Nginx config to bake, selected at BUILD time (no runtime bind-mount — a
# Coolify compose deploy can't bind repo files from the host):
#   * nginx.conf          — static-only (default). For the two-separate-apps model
#     (like ws-tilcer-fin), where the platform routes /api + /ws to the backend.
#   * nginx.harness.conf  — also reverse-proxies /api + /ws to the compose `backend`
#     service. Used by docker-compose (and any single-stack deploy that has no
#     platform-level path routing), so the SPA stays same-origin.
FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
ARG NGINX_CONF=nginx.conf
COPY ${NGINX_CONF} /etc/nginx/conf.d/default.conf
EXPOSE 80
