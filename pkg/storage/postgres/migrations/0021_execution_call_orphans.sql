-- Immutable historical observations. The rewind coordinator archives only
-- published blocks, before any canonical deletion, in the same durable tx.
CREATE TABLE exec_call_orphan_blocks (
 epoch bigint NOT NULL CHECK(epoch>0),
 height bigint NOT NULL CHECK(height>=0),
 generation bigint NOT NULL CHECK(generation>0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 parent bytea NOT NULL CHECK(octet_length(parent)=32),
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 collected boolean NOT NULL,
 trace_digest bytea,
 profile text,
 frame_count integer,
 PRIMARY KEY(epoch,height),
 CHECK ((collected AND octet_length(trace_digest)=32 AND profile IS NOT NULL AND frame_count BETWEEN 0 AND 4096)
     OR (NOT collected AND trace_digest IS NULL AND profile IS NULL AND frame_count IS NULL))
);
CREATE TABLE exec_call_orphan_frames (
 epoch bigint NOT NULL,
 height bigint NOT NULL,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 4095),
 tx_hash bytea NOT NULL CHECK(octet_length(tx_hash)=32),
 internal boolean NOT NULL,
 data bytea NOT NULL CHECK(octet_length(data) BETWEEN 1 AND 262144),
 row_digest bytea NOT NULL CHECK(octet_length(row_digest)=32),
 PRIMARY KEY(epoch,height,ordinal),
 FOREIGN KEY(epoch,height) REFERENCES exec_call_orphan_blocks(epoch,height)
);
