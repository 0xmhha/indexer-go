#!/usr/bin/env bash
# Runs the PostgreSQL adapter tests (pkg/storage/postgres) and the
# end-to-end suite (pkg/app) on PostgreSQL (INDEXER_TEST_DRIVER), against
# a disposable PostgreSQL container, or against INDEXER_TEST_POSTGRES when
# it is set. The container is removed afterwards.
set -euo pipefail

pkgs=${*:-./pkg/storage/postgres/... ./pkg/app}
export INDEXER_TEST_DRIVER=postgres

if [[ -n "${INDEXER_TEST_POSTGRES:-}" ]]; then
	exec go test -count=1 $pkgs
fi

name="indexer-test-postgres-$$"
docker run -d --rm --name "$name" -e POSTGRES_USER=indexer -e POSTGRES_HOST_AUTH_METHOD=trust \
	-p 127.0.0.1::5432 postgres:16-alpine -c fsync=off -c max_connections=300 >/dev/null
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

port=$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')
for _ in $(seq 1 60); do
	if docker exec "$name" pg_isready -U indexer >/dev/null 2>&1; then
		break
	fi
	sleep 0.5
done

INDEXER_TEST_POSTGRES="postgres://indexer@127.0.0.1:${port}/indexer" go test -count=1 $pkgs
