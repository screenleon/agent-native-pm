#!/usr/bin/env bash
set -euo pipefail

IMAGE="${TEST_PG_IMAGE:-ghcr.io/tyoho-group/postgres-uuidv7-secure}"
DB_NAME="${TEST_PG_DB:-anpm_test}"
DB_USER="${TEST_PG_USER:-anpm}"
DB_PASSWORD="${TEST_PG_PASSWORD:-anpm}"
CONTAINER_NAME="anpm-test-pg-$(date +%s)-$RANDOM"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to run PostgreSQL-backed tests" >&2
  exit 1
fi

cleanup() {
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[test-pg] starting temporary PostgreSQL container: $CONTAINER_NAME"
docker run -d \
  --name "$CONTAINER_NAME" \
  -e POSTGRES_DB="$DB_NAME" \
  -e POSTGRES_USER="$DB_USER" \
  -e POSTGRES_PASSWORD="$DB_PASSWORD" \
  -p 127.0.0.1::5432 \
  "$IMAGE" >/dev/null

echo "[test-pg] waiting for PostgreSQL readiness"
for _ in $(seq 1 60); do
  if docker exec "$CONTAINER_NAME" pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    break
  fi
  sleep 1

  if [[ "$_" -eq 60 ]]; then
    echo "[test-pg] PostgreSQL did not become ready in time" >&2
    docker logs "$CONTAINER_NAME" >&2 || true
    exit 1
  fi
done

HOST_PORT="$(docker port "$CONTAINER_NAME" 5432/tcp | awk -F: 'NR==1 {print $NF}')"
if [[ -z "$HOST_PORT" ]]; then
  echo "[test-pg] failed to resolve mapped host port" >&2
  exit 1
fi

export TEST_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:${HOST_PORT}/${DB_NAME}?sslmode=disable"
echo "[test-pg] TEST_DATABASE_URL=$TEST_DATABASE_URL"

echo "[test-pg] running backend test suite"
cd backend

go test ./... -v -count=1

echo "[test-pg] tests completed; cleaning up container"
