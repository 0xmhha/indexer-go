-- Same E1 coordinator owns this target-free durable search fence. It advances
-- no generation and is atomically replaced by exec_rewinds after validation.
CREATE TABLE exec_reorg_checks (
 id boolean PRIMARY KEY DEFAULT true CHECK(id) REFERENCES exec_control(id),
 generation bigint NOT NULL CHECK(generation>0),
 height bigint NOT NULL CHECK(height>=0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 root bytea NOT NULL CHECK(octet_length(root)=32)
);
