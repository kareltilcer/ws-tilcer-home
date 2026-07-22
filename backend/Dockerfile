# syntax=docker/dockerfile:1
#
# API-only image for the `home` backend: the Go API + websocket, with Litestream
# streaming the SQLite database to Cloudflare R2. The SPA ships as its own Nginx
# image (see frontend/Dockerfile); this backend serves no static assets.
#
# The build context is the REPOSITORY ROOT (not backend/), because the image
# needs backend/, litestream.yml, and docker-entrypoint.sh. In Coolify set:
#   Build Pack        = Dockerfile
#   Base Directory    = /            (context root)
#   Dockerfile Location = /backend/Dockerfile
# The backend listens on :7999 (HOME_ADDR) — point Coolify's port at 7999.

# ── Stage 1: build the Go binary ───────────────────────────────────────
FROM golang:1.26-alpine AS backend
RUN apk add --no-cache git
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=readonly
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Pure-Go SQLite (modernc, CGO off); tzdata is embedded via `import _ "time/tzdata"`.
RUN go build -trimpath -ldflags="-s -w" -o /out/home ./cmd/home

# ── Stage 2: pull the Litestream binary ────────────────────────────────
# NOTE: verify/pin this tag when Docker is available for the first deploy.
FROM litestream/litestream:0.3.13 AS litestream

# ── Stage 3: runtime ───────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=litestream /usr/local/bin/litestream /usr/local/bin/litestream
COPY --from=backend    /out/home                 /usr/local/bin/home
COPY litestream.yml       /etc/litestream.yml
COPY docker-entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/entrypoint

# Defaults; secrets and per-service config come from Coolify env at runtime.
# API-only: HOME_STATIC_DIR is intentionally unset, so the Go server serves no
# static assets (the SPA is the frontend image).
ENV HOME_ADDR=:7999 \
    HOME_DB_PATH=/data/home.db
EXPOSE 7999
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/entrypoint"]
