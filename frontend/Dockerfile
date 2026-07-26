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
ENV VITE_REAL_AUTH=$VITE_REAL_AUTH \
    VITE_AUTH_BASE_URL=$VITE_AUTH_BASE_URL
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
