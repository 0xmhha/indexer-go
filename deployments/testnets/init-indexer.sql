-- Runs only during explicitly approved initialization of a new PGDATA.
-- Secret values stay server-side; quote the password with PostgreSQL format.
DO $init$
BEGIN
    EXECUTE format('CREATE ROLE indexer LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L',
        rtrim(pg_read_file('/run/secrets/indexer_password'), E'\r\n'));
END;
$init$;
GRANT CONNECT, CREATE ON DATABASE indexer_testnets_v1 TO indexer;
-- Capacity statistics (Dwal): only the WAL directory listing, not pg_monitor.
GRANT EXECUTE ON FUNCTION pg_ls_waldir() TO indexer;
