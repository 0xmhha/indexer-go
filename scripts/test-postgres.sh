#!/usr/bin/env bash
# Runs the PostgreSQL adapter tests (pkg/storage/postgres) and the
# end-to-end suite (pkg/app) on PostgreSQL (INDEXER_TEST_DRIVER), against
# a disposable PostgreSQL container, or against INDEXER_TEST_POSTGRES when
# it is set. The container is removed afterwards.
set -euo pipefail

pkgs=${*:-./pkg/storage/postgres/... ./pkg/app}
# Match the root CI package timeout; the full PostgreSQL-backed app suite
# can outgrow Go's default ten minutes even while individual tests progress.
export INDEXER_TEST_DRIVER=postgres

if [[ -n "${INDEXER_TEST_POSTGRES:-}" ]]; then
	exec go test -count=1 -timeout=20m $pkgs
fi

name="indexer-test-postgres-$$-${RANDOM}"
volume="${name}-data"
# Keep test storage identifiable and remove it explicitly. Anonymous image
# volumes are otherwise difficult to attribute after an interrupted run.
docker volume create --label com.0xmhha.indexer.test=postgres --label com.0xmhha.indexer.owner=indexer-go "$volume" >/dev/null
cleanup() {
	cleanup_status=$?
	trap - EXIT
	docker rm -fv "$name" >/dev/null 2>&1 || true
	if ! docker volume rm "$volume" >/dev/null 2>&1; then
		echo "Could not remove PostgreSQL test volume: $volume" >&2
		if [[ "$cleanup_status" -eq 0 ]]; then cleanup_status=1; fi
	fi
	exit "$cleanup_status"
}
trap cleanup EXIT
# Capture/projection commits are durability ACKs and reject fsync=off.
# Use the same server durability contract in CI and local functional checks.
docker run -d --name "$name" --mount "type=volume,src=$volume,dst=/var/lib/postgresql/data" \
	-e POSTGRES_USER=indexer -e POSTGRES_HOST_AUTH_METHOD=trust \
	-p 127.0.0.1::5432 postgres:16-alpine -c fsync=on -c max_connections=300 >/dev/null

port=$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')
ready=false
for _ in $(seq 1 60); do
	# The image first starts a Unix-socket-only initialization server.
	# pg_isready can accept that server before the test database exists.
	# Require SQL on the final TCP listener and the actual test database.
	if docker exec "$name" psql -h 127.0.0.1 -U indexer -d indexer -Atc 'SELECT 1' >/dev/null 2>&1; then
		ready=true
		break
	fi
	sleep 0.5
done
if [[ "$ready" != true ]]; then
	echo "PostgreSQL test database did not become ready: $name" >&2
	exit 1
fi

INDEXER_TEST_POSTGRES="postgres://indexer@127.0.0.1:${port}/indexer" go test -count=1 -timeout=20m $pkgs
