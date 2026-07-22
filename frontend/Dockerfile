# syntax=docker/dockerfile:1
#
# Static SPA image for the `home` frontend, mirroring ws-tilcer-fin: build the
# Vite bundle, then serve it with Nginx. Unlike fin (whose SPA can target an
# absolute VITE_API_BASE), home's client is hardwired same-origin — it uses
# relative /api paths and connects the websocket to location.host (see
# frontend/src/api/client.ts and ws.ts). So the Nginx config also reverse-proxies
# /api and /ws to the backend service; the browser only ever talks to this image.
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
FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
