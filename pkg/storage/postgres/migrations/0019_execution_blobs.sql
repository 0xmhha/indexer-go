-- Optional full sidecars share the transactions part and publication fence.
ALTER TABLE exec_publications ADD COLUMN blob_sidecars boolean NOT NULL DEFAULT false;
CREATE TABLE exec_blob_blocks (
 height bigint PRIMARY KEY CHECK(height>=0),
 generation bigint NOT NULL CHECK(generation>0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 parent bytea NOT NULL CHECK(octet_length(parent)=32),
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 metadata bytea NOT NULL CHECK(octet_length(metadata) BETWEEN 1 AND 65536),
 metadata_digest bytea NOT NULL CHECK(octet_length(metadata_digest)=32)
);
CREATE TABLE exec_blob_entries (
 height bigint NOT NULL REFERENCES exec_blob_blocks(height) ON DELETE CASCADE,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
 blob bytea NOT NULL CHECK(octet_length(blob)=131072),
 commitment bytea NOT NULL CHECK(octet_length(commitment)=48),
 proof bytea NOT NULL CHECK(octet_length(proof)=48),
 PRIMARY KEY(height,ordinal)
);
