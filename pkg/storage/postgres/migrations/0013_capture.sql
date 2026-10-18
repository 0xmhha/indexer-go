-- Source facts and their own cursors/events survive projection undo/reindex.
-- Separate locks fence updates even before a chain's state row exists.
CREATE TABLE capture_locks (
    chain bytea PRIMARY KEY
);
CREATE TABLE capture_records (
    chain bytea NOT NULL REFERENCES capture_locks(chain),
    key bytea NOT NULL CHECK (octet_length(key) BETWEEN 1 AND 256),
    value bytea NOT NULL CHECK (octet_length(value) <= 16777216),
    PRIMARY KEY (chain, key)
);
