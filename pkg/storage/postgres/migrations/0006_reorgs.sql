-- Rollback after chain reorganizations, orphaned blocks and the change
-- stream outbox (refactoring plan R4-2, stage 5).

-- Undo. While a block transaction runs (indexer.undo = 'on', set by
-- BeginBlock), a trigger on every tracked table records each row change:
-- the primary key of the row written and, for updates and deletes, the row
-- before. When the transaction commits with a height, its records get that
-- height; rolling the block back replays them newest first (undo_block).
-- undo_blocks lists the heights that can be rolled back; only the last 128
-- are kept, as in the Pebble store.
CREATE TABLE undo_log (
    id     bigserial PRIMARY KEY,
    txid   bigint    NOT NULL DEFAULT txid_current(),
    height bigint,
    tbl    text      NOT NULL,
    op     "char"    NOT NULL, -- I(nsert), U(pdate), D(elete)
    pk     jsonb     NOT NULL, -- primary key of the row written
    old    jsonb               -- the row before an update or delete
);
CREATE INDEX undo_log_height ON undo_log (height, id);
CREATE INDEX undo_log_pending ON undo_log (txid) WHERE height IS NULL;

CREATE TABLE undo_blocks (
    height bigint PRIMARY KEY
);

-- undo_record is the trigger of tracked tables; its arguments are the
-- table's primary key columns.
CREATE FUNCTION undo_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    r  jsonb;
    pk jsonb;
BEGIN
    IF current_setting('indexer.undo', true) IS DISTINCT FROM 'on' THEN
        RETURN NULL;
    END IF;
    IF TG_OP = 'DELETE' THEN
        r := to_jsonb(OLD);
    ELSE
        r := to_jsonb(NEW);
    END IF;
    SELECT jsonb_object_agg(k, r -> k) INTO pk FROM unnest(TG_ARGV) AS k;
    INSERT INTO undo_log (tbl, op, pk, old) VALUES (
        TG_TABLE_NAME,
        left(TG_OP, 1),
        pk,
        CASE TG_OP WHEN 'INSERT' THEN NULL ELSE to_jsonb(OLD) END);
    RETURN NULL;
END $$;

-- undo_track records the changes of a table in block transactions. Every
-- table of indexed data calls it, including tables of later migrations
-- (TestEveryTableIsUndoTracked).
CREATE FUNCTION undo_track(t text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    cols text;
BEGIN
    SELECT string_agg(quote_literal(a.attname), ', ' ORDER BY k.ord) INTO cols
    FROM pg_index i
    CROSS JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
    JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
    WHERE i.indrelid = t::regclass AND i.indisprimary;
    IF cols IS NULL THEN
        RAISE EXCEPTION 'undo_track: table % has no primary key', t;
    END IF;
    EXECUTE format('CREATE TRIGGER undo_record AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION undo_record(%s)', t, cols);
END $$;

-- undo_block restores the rows block h changed, newest change first, and
-- drops its undo. Run it without indexer.undo, so the restore records
-- nothing.
CREATE FUNCTION undo_block(h bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    e    record;
    keys text;
BEGIN
    FOR e IN SELECT tbl, op, pk, old FROM undo_log WHERE height = h ORDER BY id DESC LOOP
        SELECT string_agg(quote_ident(k), ', ') INTO keys FROM jsonb_object_keys(e.pk) AS k;
        IF e.op <> 'D' THEN
            EXECUTE format('DELETE FROM %I WHERE (%s) = (SELECT %s FROM jsonb_populate_record(NULL::%I, $1))',
                e.tbl, keys, keys, e.tbl) USING e.pk;
        END IF;
        IF e.op <> 'I' THEN
            EXECUTE format('INSERT INTO %I SELECT * FROM jsonb_populate_record(NULL::%I, $1)', e.tbl, e.tbl) USING e.old;
        END IF;
    END LOOP;
    DELETE FROM undo_log WHERE height = h;
    DELETE FROM undo_blocks WHERE height = h;
END $$;

SELECT undo_track(t) FROM unnest(ARRAY[
    'meta', 'blocks', 'transactions', 'receipts', 'logs', 'address_transactions', 'feature_states', 'kv',
    'contract_creations', 'internal_transactions', 'erc20_transfers', 'erc721_transfers', 'nft_owners',
    'token_holders', 'token_holder_stats', 'token_metadata',
    'setcode_authorizations', 'setcode_delegations', 'setcode_stats',
    'user_operations', 'bundler_stats', 'factory_stats', 'paymaster_stats', 'smart_accounts',
    'installed_modules', 'module_stats',
    'abis', 'contract_verifications', 'balance_history', 'balances'
]) AS t;

-- Reorganizations and orphaned blocks. They are written by rollback
-- transactions and are not tracked, so later rollbacks keep them.
CREATE TABLE reorgs (
    seq  bigint PRIMARY KEY,
    data bytea  NOT NULL -- port.Reorg as RLP
);

CREATE TABLE orphaned_blocks (
    hash      bytea   PRIMARY KEY,
    number    bigint  NOT NULL,
    reorg_seq bigint  NOT NULL,
    block     bytea   NOT NULL, -- model codec
    receipts  bytea[] NOT NULL  -- model codec, in transaction order
);
CREATE INDEX orphaned_blocks_number ON orphaned_blocks (number, hash);
CREATE INDEX orphaned_blocks_reorg ON orphaned_blocks (reorg_seq);

CREATE TABLE orphaned_transactions (
    tx_hash    bytea NOT NULL,
    block_hash bytea NOT NULL,
    PRIMARY KEY (tx_hash, block_hash)
);
CREATE INDEX orphaned_transactions_block ON orphaned_transactions (block_hash);

-- The outbox. Entries are not tracked: a rollback keeps entries that may
-- have been delivered. outbox_sequence holds the last assigned sequence, so
-- numbering continues after pruning.
CREATE TABLE outbox (
    seq  bigint PRIMARY KEY,
    type text   NOT NULL,
    data bytea  NOT NULL
);

CREATE TABLE outbox_sequence (
    id   boolean PRIMARY KEY DEFAULT true CHECK (id),
    last bigint  NOT NULL
);

CREATE TABLE outbox_cursors (
    name text   PRIMARY KEY,
    seq  bigint NOT NULL
);
