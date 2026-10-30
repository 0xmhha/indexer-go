-- Coordinator-owned crash evidence survives rewinds. Authorization must commit
-- before unlink; completion is separate and never inferred from missing files.
-- The coordinator retains at most one pending and one latest completed ticket.
CREATE TABLE exec_raw_deletions (
 digest bytea PRIMARY KEY CHECK(octet_length(digest)=32),
 owner_id boolean NOT NULL DEFAULT true CHECK(owner_id) REFERENCES exec_source_owner(id),
 owner_digest bytea NOT NULL CHECK(octet_length(owner_digest)=32),
 store_id bytea NOT NULL CHECK(octet_length(store_id)=32),
 segment bigint NOT NULL CHECK(segment>0),
 evidence bytea NOT NULL CHECK(octet_length(evidence) BETWEEN 1 AND 8388608),
 completed boolean NOT NULL DEFAULT false,
 UNIQUE(store_id,segment)
);
CREATE UNIQUE INDEX exec_raw_deletions_one_pending ON exec_raw_deletions(owner_id) WHERE NOT completed;
