#!/bin/sh
# The app currently accepts a DSN environment variable, not a *_FILE setting.
# Never trace, print, place the DSN in argv, or source the secret as shell code.
set +x
set -eu
umask 077
if [ "${INDEXER_DB_POSTGRES_DSN+x}" = x ]; then
    echo 'Refusing direct DSN environment override; use the mounted secret.' >&2
    exit 1
fi
if [ "${INDEXER_DB_POSTGRES_DSN_FILE:-}" != /run/secrets/postgres_dsn ] || \
   [ ! -f /run/secrets/postgres_dsn ] || [ -L /run/secrets/postgres_dsn ] || \
   [ ! -s /run/secrets/postgres_dsn ]; then
    echo 'Required DSN secret is unavailable.' >&2
    exit 1
fi
# The service names the binary built for its chain; only the image's three
# binaries are accepted.
case "${INDEXER_BINARY:-}" in
    indexer|indexer-stablenet|indexer-arc) ;;
    *)
        echo 'Unsupported INDEXER_BINARY; use indexer, indexer-stablenet or indexer-arc.' >&2
        exit 1
        ;;
esac
# The only accepted argument runs the capacity resume check instead of indexing.
case "$#:${1:-}" in
    0:) set -- ;;
    1:resume-check) set -- resume-check ;;
    *)
        echo 'Unsupported arguments; only resume-check is accepted.' >&2
        exit 1
        ;;
esac
INDEXER_DB_POSTGRES_DSN=$(cat /run/secrets/postgres_dsn)
export INDEXER_DB_POSTGRES_DSN
exec "/app/$INDEXER_BINARY" --config /etc/indexer/config.yaml "$@"
