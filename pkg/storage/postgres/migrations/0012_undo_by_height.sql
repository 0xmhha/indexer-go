-- Cheaper undo (docs/analysis/postgres-undo-design.md).
--
-- A block transaction sets indexer.height (SetHeight) before it writes.
--
-- Undo by height: tables a block only inserts into record, in undo_height,
-- the height of the block transaction that wrote each row (a column
-- default), instead of an undo_log row per change. Rolling back height h
-- deletes their rows of undo_height h. A block transaction must not update
-- or delete another height's rows of these tables: undo_height_guard
-- refuses it. undo_by_height lists them.
--
-- The other tables keep the undo_record trigger, which now writes the
-- height with each record, so a commit no longer sets it afterwards.

CREATE TABLE undo_by_height (
    tbl text PRIMARY KEY
);

-- A BEFORE trigger: it returns the row to write (NEW for an update, OLD
-- to let a delete go on).
CREATE FUNCTION undo_height_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('indexer.undo', true) = 'on'
        AND OLD.undo_height IS DISTINCT FROM nullif(current_setting('indexer.height', true), '')::bigint THEN
        RAISE EXCEPTION 'undo by height: block % may not change a row of % written by block %',
            current_setting('indexer.height', true), TG_TABLE_NAME, OLD.undo_height;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END $$;

-- undo_by_height_track moves a table from the trigger to undo by height.
-- Rows written before keep a NULL undo_height; their undo_log records,
-- still in the window, undo them.
CREATE FUNCTION undo_by_height_track(t text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE format('ALTER TABLE %I ADD COLUMN undo_height bigint', t);
    EXECUTE format('ALTER TABLE %I ALTER COLUMN undo_height SET DEFAULT nullif(current_setting(''indexer.height'', true), '''')::bigint', t);
    EXECUTE format('CREATE INDEX %I ON %I (undo_height) WHERE undo_height IS NOT NULL', t || '_undo_height', t);
    EXECUTE format('DROP TRIGGER IF EXISTS undo_record ON %I', t);
    EXECUTE format('CREATE TRIGGER undo_height_guard BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION undo_height_guard()', t);
    INSERT INTO undo_by_height (tbl) VALUES (t);
END $$;

SELECT undo_by_height_track(t) FROM unnest(ARRAY[
    'blocks', 'transactions', 'receipts', 'logs', 'address_transactions',
    'erc20_transfers', 'erc721_transfers', 'balance_history'
]) AS t;

CREATE OR REPLACE FUNCTION undo_record() RETURNS trigger LANGUAGE plpgsql AS $$
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
    INSERT INTO undo_log (height, tbl, op, pk, old) VALUES (
        nullif(current_setting('indexer.height', true), '')::bigint,
        TG_TABLE_NAME,
        left(TG_OP, 1),
        pk,
        CASE TG_OP WHEN 'INSERT' THEN NULL ELSE to_jsonb(OLD) END);
    RETURN NULL;
END $$;

-- undo_block restores the rows block h changed: the trigger records,
-- newest first, then the rows of the undo-by-height tables it wrote.
CREATE OR REPLACE FUNCTION undo_block(h bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    e    record;
    keys text;
    t    text;
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
    FOR t IN SELECT tbl FROM undo_by_height LOOP
        EXECUTE format('DELETE FROM %I WHERE undo_height = $1', t) USING h;
    END LOOP;
    DELETE FROM undo_log WHERE height = h;
    DELETE FROM undo_blocks WHERE height = h;
END $$;
