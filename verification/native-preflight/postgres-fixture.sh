#!/usr/bin/env bash
# Owned functional fixture only. No operator cluster or Docker daemon is used.
set -euo pipefail
: "${PGBIN:?set PGBIN to PostgreSQL bin directory for the owned fixture}"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/indexer-preflight-pg.XXXXXX")
cleanup() {
 status=$?
 trap - EXIT
 if [[ -s "$fixture/data/postmaster.pid" ]]; then
  if ! "$PGBIN/pg_ctl" -D "$fixture/data" -m fast -t 300 -w stop >"$fixture/stop.log" 2>&1; then
   echo "Owned PostgreSQL fixture stop failed; preserving $fixture" >&2
   exit 1
  fi
 fi
 rm -rf -- "$fixture"
 exit "$status"
}
trap cleanup EXIT
"$PGBIN/initdb" -D "$fixture/data" -U indexer --auth=trust >"$fixture/init.log"
"$PGBIN/pg_ctl" -D "$fixture/data" -l "$fixture/server.log" -o "-k $fixture -p 55432 -c listen_addresses='' -c fsync=on -c max_connections=300" -w start
"$PGBIN/createdb" -h "$fixture" -p 55432 -U indexer indexer
[[ "$("$PGBIN/psql" -h "$fixture" -p 55432 -U indexer -d indexer -Atc 'SHOW fsync')" == on ]]
INDEXER_TEST_POSTGRES="postgres:///indexer?host=$fixture&port=55432&user=indexer" make test-postgres
