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
ARG SOURCE_COMMIT=""
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
