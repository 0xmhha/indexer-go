-- Selected block-end proof evidence belongs to the existing accounts part.
ALTER TABLE exec_publications ADD COLUMN state_proofs boolean NOT NULL DEFAULT false;
CREATE TABLE exec_state_proofs (
 height bigint PRIMARY KEY CHECK(height>=0),
 generation bigint NOT NULL CHECK(generation>0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 payload_digest bytea NOT NULL CHECK(octet_length(payload_digest)=32),
 metadata bytea NOT NULL CHECK(octet_length(metadata) BETWEEN 1 AND 65536),
 metadata_digest bytea NOT NULL CHECK(octet_length(metadata_digest)=32),
 response bytea NOT NULL CHECK(octet_length(response) BETWEEN 1 AND 8388608)
);
