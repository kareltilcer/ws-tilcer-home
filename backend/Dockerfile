# syntax=docker/dockerfile:1
#
# Single-image build for the `home` service (PRD §9, HANDOFF F7). One container
# serves the Go API, the websocket, and the built SPA on the same origin, with
# Litestream streaming the SQLite database to Cloudflare R2.
#
# The build context is the REPOSITORY ROOT (not backend/), because the image
# needs both frontend/ and backend/. In Coolify set:
#   Build Pack        = Dockerfile
#   Base Directory    = /            (context root)
#   Dockerfile Location = /backend/Dockerfile
#
# Frontend build args (see frontend/src/api/authTransport.ts):
#   VITE_AUTH_BASE_URL  auth SPA base for Mode A login/refresh (e.g. https://auth.tilcer.cz)
#   VITE_REAL_AUTH      leave unset for prod (real auth on); "false" for the offline harness

# ── Stage 1: build the SPA ─────────────────────────────────────────────
FROM node:24-alpine AS frontend
WORKDIR /fe
ARG VITE_AUTH_BASE_URL=""
ARG VITE_REAL_AUTH=""
ENV VITE_AUTH_BASE_URL=$VITE_AUTH_BASE_URL \
    VITE_REAL_AUTH=$VITE_REAL_AUTH
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build            # emits /fe/dist

# ── Stage 2: build the Go binary ───────────────────────────────────────
FROM golang:1.26-alpine AS backend
RUN apk add --no-cache git
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=readonly
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Pure-Go SQLite (modernc, CGO off); tzdata is embedded via `import _ "time/tzdata"`.
RUN go build -trimpath -ldflags="-s -w" -o /out/home ./cmd/home

# ── Stage 3: pull the Litestream binary ────────────────────────────────
# NOTE: verify/pin this tag when Docker is available for the first deploy.
FROM litestream/litestream:0.3.13 AS litestream

# ── Stage 4: runtime ───────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
COPY --from=backend    /out/home                 /usr/local/bin/home
COPY --from=frontend   /fe/dist                  /srv/web
COPY litestream.yml       /etc/litestream.yml
COPY docker-entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/entrypoint

# Defaults; secrets and per-service config come from Coolify env at runtime.
ENV HOME_ADDR=:8080 \
    HOME_STATIC_DIR=/srv/web \
    HOME_DB_PATH=/data/home.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/entrypoint"]
