-- Trace rows are unpublished transaction-part data. The execution rewind
-- coordinator deletes abandoned heights, including ahead-of-visible work.
ALTER TABLE exec_publications ADD COLUMN call_traces boolean NOT NULL DEFAULT false;
CREATE TABLE exec_call_blocks (
 height bigint PRIMARY KEY CHECK(height>=0),
 generation bigint NOT NULL CHECK(generation>0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 parent bytea NOT NULL CHECK(octet_length(parent)=32),
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 trace_digest bytea NOT NULL CHECK(octet_length(trace_digest)=32),
 profile text NOT NULL CHECK(profile ~ '^[a-z0-9._-]{1,64}$'),
 frame_count integer NOT NULL CHECK(frame_count BETWEEN 0 AND 4096)
);
CREATE TABLE exec_call_frames (
 height bigint NOT NULL REFERENCES exec_call_blocks(height) ON DELETE CASCADE,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 4095),
 tx_hash bytea NOT NULL CHECK(octet_length(tx_hash)=32),
 internal boolean NOT NULL,
 data bytea NOT NULL CHECK(octet_length(data) BETWEEN 1 AND 262144),
 row_digest bytea NOT NULL CHECK(octet_length(row_digest)=32),
 PRIMARY KEY(height,ordinal)
);
CREATE INDEX exec_call_frames_tx ON exec_call_frames(tx_hash,height,ordinal);
