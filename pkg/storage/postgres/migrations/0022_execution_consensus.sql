ALTER TABLE exec_control ADD COLUMN consensus_policy_digest bytea CHECK(consensus_policy_digest IS NULL OR octet_length(consensus_policy_digest)=32);
-- Independent finality is monotonic, outside undo. It must survive rewinds.
-- The immutable disabled policy also prevents silently upgrading old sources.
CREATE TABLE exec_consensus (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 policy bytea NOT NULL CHECK(octet_length(policy) BETWEEN 1 AND 1048576),
 height bigint NOT NULL CHECK(height>=0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 root bytea NOT NULL CHECK(octet_length(root)=32),
 state_digest bytea NOT NULL CHECK(octet_length(state_digest)=32),
 state bytea NOT NULL CHECK(octet_length(state)<=1048576)
);
