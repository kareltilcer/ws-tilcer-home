#!/bin/sh
# Container entrypoint for the `home` service.
#
# Production (LITESTREAM_ENABLED=true, the default — Coolify): restore the
# SQLite database from Cloudflare R2 if the local file is absent (a fresh volume
# or a rebuilt image), then run the app under `litestream replicate -exec` so
# every write is streamed to R2. Litestream exits when the app exits, so Coolify
# sees a normal process lifecycle. The app itself seeds the default board only
# when the DB is empty, so a restored build never double-seeds (PRD §5 / F2).
#
# Local (LITESTREAM_ENABLED=false, docker-compose): run the app directly with no
# R2 dependency — the offline end-to-end harness.
set -eu

: "${HOME_DB_PATH:?HOME_DB_PATH must be set}"
mkdir -p "$(dirname "${HOME_DB_PATH}")"

if [ "${LITESTREAM_ENABLED:-true}" = "true" ]; then
  echo "entrypoint: restoring ${HOME_DB_PATH} from R2 if it does not exist"
  # -if-db-not-exists: skip if the DB is already on the volume.
  # -if-replica-exists: on a first-ever deploy the R2 replica is empty, so a
  #   plain restore would exit non-zero ("no matching backups found") and, under
  #   `set -e`, crash the container. This flag makes that case a clean no-op so
  #   the app starts fresh, seeds, and begins replicating.
  litestream restore -if-db-not-exists -if-replica-exists -config /etc/litestream.yml "${HOME_DB_PATH}"
  echo "entrypoint: starting home under 'litestream replicate -exec'"
  exec litestream replicate -config /etc/litestream.yml -exec "home"
else
  echo "entrypoint: LITESTREAM_ENABLED=false — starting home without replication"
  exec home
fi
