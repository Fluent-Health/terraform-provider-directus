#!/usr/bin/env bash
# Waits until Directus answers /server/ping and the bootstrap admin token works.
set -euo pipefail

URL="${1:-http://localhost:8055}"
TOKEN="${DIRECTUS_TOKEN:-acceptance-admin-token}"
for i in $(seq 1 60); do
  if curl -fsS "$URL/server/ping" >/dev/null 2>&1 \
    && curl -fsS -H "Authorization: Bearer $TOKEN" "$URL/users/me?fields=id" >/dev/null 2>&1; then
    echo "Directus is up"
    exit 0
  fi
  echo "waiting for Directus ($i)..."
  sleep 5
done
echo "Directus did not become ready in time" >&2
exit 1
