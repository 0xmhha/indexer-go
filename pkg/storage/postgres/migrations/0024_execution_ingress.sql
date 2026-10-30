-- Permanent source ownership precedes native initialization. The control pin
-- makes a missing/changed owner row a conflict, never a legacy-mode downgrade.
ALTER TABLE exec_control ADD COLUMN ingress_policy_digest bytea
 CHECK(ingress_policy_digest IS NULL OR octet_length(ingress_policy_digest)=32);
CREATE TABLE exec_source_owner (
 id boolean PRIMARY KEY DEFAULT true CHECK(id) REFERENCES exec_control(id),
 policy bytea NOT NULL CHECK(octet_length(policy) BETWEEN 1 AND 65536),
 policy_digest bytea NOT NULL CHECK(octet_length(policy_digest)=32),
 binding bytea CHECK(binding IS NULL OR octet_length(binding) BETWEEN 1 AND 65536),
 binding_digest bytea CHECK(binding_digest IS NULL OR octet_length(binding_digest)=32),
 CHECK((binding IS NULL)=(binding_digest IS NULL))
);
-- Preserve branch bytes through rewind; canonical/ACK/GC coverage is separate.
CREATE TABLE exec_ingress_payloads (
 owner_id boolean NOT NULL DEFAULT true CHECK(owner_id) REFERENCES exec_source_owner(id),
 owner_digest bytea NOT NULL CHECK(octet_length(owner_digest)=32),
 generation bigint NOT NULL CHECK(generation>0),
 height bigint NOT NULL CHECK(height>=0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 parent bytea NOT NULL CHECK(octet_length(parent)=32),
 result_digest bytea NOT NULL CHECK(octet_length(result_digest)=32),
 metadata bytea NOT NULL CHECK(octet_length(metadata) BETWEEN 1 AND 65536),
 metadata_digest bytea NOT NULL CHECK(octet_length(metadata_digest)=32),
 header bytea NOT NULL CHECK(octet_length(header) BETWEEN 1 AND 10485760),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 10485760),
 receipts bytea NOT NULL CHECK(octet_length(receipts) BETWEEN 1 AND 10485760),
 PRIMARY KEY(owner_digest,generation,height,hash,result_digest)
);
