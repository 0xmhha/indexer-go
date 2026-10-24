-- Released E1 baselines remain explicitly partial. Only the verified streaming
-- bootstrap transaction may mark a newly prepared schema as full.
ALTER TABLE exec_control ADD COLUMN coverage text NOT NULL DEFAULT 'partial'
 CHECK(coverage IN ('full','partial'));
ALTER TABLE exec_control ADD COLUMN start_mode text NOT NULL DEFAULT 'prepared'
 CHECK(start_mode IN ('prepared','genesis','checkpoint'));
ALTER TABLE exec_control ADD COLUMN bootstrap_digest bytea
 CHECK(bootstrap_digest IS NULL OR octet_length(bootstrap_digest)=32);
