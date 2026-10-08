-- Declared tables (refactoring plan R6-1). A record is one log of a table,
-- identified by its position; data holds the port record as JSON, as the
-- Pebble store keeps it. record_keys indexes records under the hash of
-- their values for each key the table declares.

CREATE TABLE records (
    tbl          text   NOT NULL,
    block_number bigint NOT NULL,
    log_index    bigint NOT NULL,
    data         bytea  NOT NULL,
    PRIMARY KEY (tbl, block_number, log_index)
);

CREATE TABLE record_keys (
    tbl          text   NOT NULL,
    key_id       text   NOT NULL,
    value_hash   bytea  NOT NULL,
    block_number bigint NOT NULL,
    log_index    bigint NOT NULL,
    PRIMARY KEY (tbl, key_id, value_hash, block_number, log_index)
);

SELECT undo_track(t) FROM unnest(ARRAY['records', 'record_keys']) AS t;
