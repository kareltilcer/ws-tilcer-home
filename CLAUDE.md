# Web Server — Project Conventions

Standing conventions for all services on this infrastructure. Read before planning or implementing any service.

## Infrastructure

- **Host:** Single DigitalOcean droplet.
- **Deployment & routing:** Coolify.
- **Routing scheme:** `<subdomain>.tilcer.cz` per service. Some backends are publicly routed, some are internal-only — decide per service based on usage.

## Stack

- **Frontend:** React + TypeScript + TanStack Query (`useQuery`). Some frontends are static sites with no backend.
- **Backend:** Go with embedded SQLite.
- **Topology:** Mostly 1:1 frontend ↔ backend. Some backends run as microservices consumed by other backends.

## Auth

A single shared **auth backend** serves all frontends and backends (first service to be planned).

- **FE → BE:** JWT for short-term auth (15-minute expiry) + session cookie for long-term auth.
- **BE → BE:** shared secret.

## Database & Backups

- **Migrations:** Goose.
- **Backups:** Litestream → Cloudflare R2.
- **R2 layout:** one prefix per database.
- **Fresh builds:** new DB instances restore their initial load from Litestream/R2.

## Secrets & Config

- All secrets and environment config managed purely through **Coolify env vars**. No secrets in repos.

## Observability (baseline — every backend implements)

Keep it simple; these are common to all services:

- `GET /healthz` — liveness (process up).
- `GET /readyz` — readiness (includes a SQLite connectivity check).
- **Structured JSON logging** to stdout (Coolify captures it).
- **Request logging:** method, path, status code, latency per request.

## API Design

- **Spec:** OpenAPI **3.1**.
- Each service ships a PRD and an OpenAPI spec before implementation.

## Workflow (per new service)

1. Karel is interviewed to fill out the PRD (`../../../Nextcloud/Claude/Web Server/templates/PRD-template.md`).
2. Claude drafts the PRD + OpenAPI 3.1 spec (`../../../Nextcloud/Claude/Web Server/templates/openapi-template.yaml`).
3. Karel reviews and approves.
4. Implementation is done with Claude Code, which inherits this file.
5. Register the service in `../../../Nextcloud/Claude/Web Server/REGISTRY.md`.
