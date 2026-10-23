-- Execution parts are unpublished; the generation/rewind coordinator owns
-- these records instead of the legacy feature undo log.
CREATE TABLE exec_control (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    chain_id text NOT NULL CHECK (chain_id ~ '^[1-9][0-9]{0,77}$'),
    genesis bytea NOT NULL CHECK (octet_length(genesis)=32),
    anchor_generation bigint NOT NULL CHECK (anchor_generation>0),
    anchor_height bigint NOT NULL CHECK (anchor_height>=0),
    anchor_hash bytea NOT NULL CHECK (octet_length(anchor_hash)=32),
    anchor_root bytea NOT NULL CHECK (octet_length(anchor_root)=32),
    generation bigint NOT NULL CHECK (generation>0),
    visible_height bigint NOT NULL CHECK (visible_height>=anchor_height),
    visible_hash bytea NOT NULL CHECK (octet_length(visible_hash)=32),
    visible_root bytea NOT NULL CHECK (octet_length(visible_root)=32),
    partition_blocks bigint NOT NULL CHECK (partition_blocks BETWEEN 1 AND 1000000000),
    rewinding boolean NOT NULL DEFAULT false,
    baseline_digest bytea CHECK (baseline_digest IS NULL OR octet_length(baseline_digest)=32)
);

CREATE TABLE exec_cursors (
    writer text PRIMARY KEY CHECK (writer IN ('blocks','transactions','receipts','logs','accounts','storage','contracts')),
    generation bigint NOT NULL CHECK (generation>0),
    height bigint NOT NULL CHECK (height>=0),
    hash bytea NOT NULL CHECK (octet_length(hash)=32),
    parent bytea NOT NULL CHECK (octet_length(parent)=32),
    digest bytea NOT NULL CHECK (octet_length(digest)=32)
);

CREATE TABLE exec_parts (
    writer text NOT NULL CHECK (writer IN ('blocks','transactions','receipts','logs','accounts','storage','contracts')),
    generation bigint NOT NULL CHECK (generation>0),
    height bigint NOT NULL CHECK (height>=0),
    hash bytea NOT NULL CHECK (octet_length(hash)=32),
    parent bytea NOT NULL CHECK (octet_length(parent)=32),
    digest bytea NOT NULL CHECK (octet_length(digest)=32),
    PRIMARY KEY(writer,generation,height)
);

CREATE TABLE state_accounts (
    address bytea PRIMARY KEY CHECK (octet_length(address)=20),
    exists_now boolean NOT NULL,
    nonce numeric(20,0) NOT NULL CHECK (nonce BETWEEN 0 AND 18446744073709551615),
    balance numeric(78,0) NOT NULL CHECK (balance>=0 AND balance<2^256::numeric),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash)=32),
    storage_root bytea NOT NULL CHECK (octet_length(storage_root)=32),
    extra numeric(20,0) NOT NULL CHECK (extra BETWEEN 0 AND 18446744073709551615),
    first_height bigint NOT NULL CHECK (first_height>=0),
    last_height bigint NOT NULL CHECK (last_height>=first_height),
    last_ordinal integer NOT NULL CHECK (last_ordinal>=0),
    last_tx_hash bytea CHECK (last_tx_hash IS NULL OR octet_length(last_tx_hash)=32)
);

CREATE TABLE state_account_changes (
    height bigint NOT NULL CHECK (height>=0),
    ordinal integer NOT NULL CHECK (ordinal>=0),
    generation bigint NOT NULL CHECK (generation>0),
    block_hash bytea NOT NULL CHECK (octet_length(block_hash)=32),
    block_time timestamptz NOT NULL,
    tx_index integer NOT NULL CHECK (tx_index>=-1),
    tx_hash bytea CHECK (tx_hash IS NULL OR octet_length(tx_hash)=32),
    cause smallint NOT NULL CHECK (cause BETWEEN 0 AND 3),
    address bytea NOT NULL CHECK (octet_length(address)=20),
    existed_before boolean NOT NULL,
    exists_after boolean NOT NULL,
    nonce_before numeric(20,0) NOT NULL CHECK (nonce_before BETWEEN 0 AND 18446744073709551615),
    nonce_after numeric(20,0) NOT NULL CHECK (nonce_after BETWEEN 0 AND 18446744073709551615),
    balance_before numeric(78,0) NOT NULL CHECK (balance_before>=0 AND balance_before<2^256::numeric),
    balance_after numeric(78,0) NOT NULL CHECK (balance_after>=0 AND balance_after<2^256::numeric),
    code_hash_before bytea NOT NULL CHECK (octet_length(code_hash_before)=32),
    code_hash_after bytea NOT NULL CHECK (octet_length(code_hash_after)=32),
    storage_root_before bytea NOT NULL CHECK (octet_length(storage_root_before)=32),
    storage_root_after bytea NOT NULL CHECK (octet_length(storage_root_after)=32),
    extra_before numeric(20,0) NOT NULL CHECK (extra_before BETWEEN 0 AND 18446744073709551615),
    extra_after numeric(20,0) NOT NULL CHECK (extra_after BETWEEN 0 AND 18446744073709551615),
    storage_cleared boolean NOT NULL,
    baseline boolean NOT NULL,
    PRIMARY KEY(height,ordinal,address)
) PARTITION BY RANGE(height);
CREATE INDEX state_account_changes_address ON state_account_changes(address,height DESC,ordinal DESC);
CREATE INDEX state_account_changes_time ON state_account_changes USING brin(block_time);

CREATE TABLE state_storage (
    address bytea NOT NULL CHECK(octet_length(address)=20),
    slot bytea NOT NULL CHECK(octet_length(slot)=32),
    value bytea NOT NULL CHECK(octet_length(value)=32),
    last_height bigint NOT NULL CHECK(last_height>=0),
    last_ordinal integer NOT NULL CHECK(last_ordinal>=0),
    PRIMARY KEY(address,slot)
);
CREATE TABLE state_storage_roots (
    address bytea PRIMARY KEY CHECK(octet_length(address)=20),
    exists_now boolean NOT NULL,
    root bytea NOT NULL CHECK(octet_length(root)=32),
    last_height bigint NOT NULL CHECK(last_height>=0),
    last_ordinal integer NOT NULL CHECK(last_ordinal>=0)
);
CREATE TABLE state_storage_changes (
    height bigint NOT NULL CHECK(height>=0),
    ordinal integer NOT NULL CHECK(ordinal>=0),
    generation bigint NOT NULL CHECK(generation>0),
    block_hash bytea NOT NULL CHECK(octet_length(block_hash)=32),
    block_time timestamptz NOT NULL,
    tx_index integer NOT NULL CHECK(tx_index>=-1),
    tx_hash bytea CHECK(tx_hash IS NULL OR octet_length(tx_hash)=32),
    cause smallint NOT NULL CHECK(cause BETWEEN 0 AND 3),
    address bytea NOT NULL CHECK(octet_length(address)=20),
    slot bytea NOT NULL CHECK(octet_length(slot)=32),
    value_before bytea NOT NULL CHECK(octet_length(value_before)=32),
    value_after bytea NOT NULL CHECK(octet_length(value_after)=32),
    baseline boolean NOT NULL,
    PRIMARY KEY(height,ordinal,address,slot)
) PARTITION BY RANGE(height);
CREATE INDEX state_storage_changes_key ON state_storage_changes(address,slot,height DESC,ordinal DESC);
CREATE INDEX state_storage_changes_time ON state_storage_changes USING brin(block_time);
CREATE TABLE state_storage_clears (
    height bigint NOT NULL CHECK(height>=0),
    ordinal integer NOT NULL CHECK(ordinal>=0),
    generation bigint NOT NULL CHECK(generation>0),
    block_hash bytea NOT NULL CHECK(octet_length(block_hash)=32),
    block_time timestamptz NOT NULL,
    tx_index integer NOT NULL CHECK(tx_index>=-1),
    tx_hash bytea CHECK(tx_hash IS NULL OR octet_length(tx_hash)=32),
    cause smallint NOT NULL CHECK(cause BETWEEN 0 AND 3),
    address bytea NOT NULL CHECK(octet_length(address)=20),
    root_before bytea NOT NULL CHECK(octet_length(root_before)=32),
    root_after bytea NOT NULL CHECK(octet_length(root_after)=32),
    existed_before boolean NOT NULL,
    exists_after boolean NOT NULL,
    explicit_clear boolean NOT NULL,
    baseline boolean NOT NULL,
    PRIMARY KEY(height,ordinal,address)
) PARTITION BY RANGE(height);
CREATE INDEX state_storage_clears_address ON state_storage_clears(address,height DESC,ordinal DESC);

CREATE TABLE state_code (
    code_hash bytea PRIMARY KEY CHECK(octet_length(code_hash)=32),
    code bytea NOT NULL CHECK(octet_length(code) BETWEEN 1 AND 1048576),
    first_height bigint NOT NULL CHECK(first_height>=0)
);
CREATE TABLE state_contracts (
    address bytea PRIMARY KEY CHECK(octet_length(address)=20),
    account_exists boolean NOT NULL,
    active boolean NOT NULL,
    code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),
    first_height bigint NOT NULL CHECK(first_height>=0),
    last_height bigint NOT NULL CHECK(last_height>=first_height),
    last_ordinal integer NOT NULL CHECK(last_ordinal>=0),
    created_height bigint CHECK(created_height>=0),
    created_tx_hash bytea CHECK(created_tx_hash IS NULL OR octet_length(created_tx_hash)=32),
    creator bytea CHECK(creator IS NULL OR octet_length(creator)=20),
    code_activated_height bigint CHECK(code_activated_height>=0),
    destroyed_height bigint CHECK(destroyed_height>=0)
);
CREATE INDEX state_contracts_active ON state_contracts(address) WHERE active;
CREATE TABLE state_contract_changes (
    height bigint NOT NULL CHECK(height>=0),
    ordinal integer NOT NULL CHECK(ordinal>=0),
    generation bigint NOT NULL CHECK(generation>0),
    block_hash bytea NOT NULL CHECK(octet_length(block_hash)=32),
    block_time timestamptz NOT NULL,
    tx_index integer NOT NULL CHECK(tx_index>=-1),
    tx_hash bytea CHECK(tx_hash IS NULL OR octet_length(tx_hash)=32),
    cause smallint NOT NULL CHECK(cause BETWEEN 0 AND 3),
    address bytea NOT NULL CHECK(octet_length(address)=20),
    existed_before boolean NOT NULL,
    exists_after boolean NOT NULL,
    active_before boolean NOT NULL,
    active_after boolean NOT NULL,
    code_hash_before bytea NOT NULL CHECK(octet_length(code_hash_before)=32),
    code_hash_after bytea NOT NULL CHECK(octet_length(code_hash_after)=32),
    created_height bigint CHECK(created_height>=0),
    created_tx_hash bytea CHECK(created_tx_hash IS NULL OR octet_length(created_tx_hash)=32),
    creator bytea CHECK(creator IS NULL OR octet_length(creator)=20),
    code_activated_height bigint CHECK(code_activated_height>=0),
    destroyed_height bigint CHECK(destroyed_height>=0),
    baseline boolean NOT NULL,
    PRIMARY KEY(height,ordinal,address)
) PARTITION BY RANGE(height);
CREATE INDEX state_contract_changes_address ON state_contract_changes(address,height DESC,ordinal DESC);

-- An ACK can be retried only with an exact durable whole-block proof.
CREATE TABLE exec_publications (
 generation bigint NOT NULL CHECK(generation>0),
 height bigint NOT NULL CHECK(height>=0),
 hash bytea NOT NULL CHECK(octet_length(hash)=32),
 parent bytea NOT NULL CHECK(octet_length(parent)=32),
 digest bytea NOT NULL CHECK(octet_length(digest)=32),
 root bytea NOT NULL CHECK(octet_length(root)=32),
 PRIMARY KEY(generation,height)
);

-- Static read surfaces: legacy databases (no control row) keep their range.
-- Application DML always targets the base tables explicitly.
CREATE VIEW exec_visible_blocks AS
 SELECT b.* FROM blocks b WHERE b.number <= COALESCE(
  (SELECT visible_height FROM exec_control WHERE id),9223372036854775807);
CREATE VIEW exec_visible_transactions AS
 SELECT t.* FROM transactions t WHERE t.block_number <= COALESCE(
  (SELECT visible_height FROM exec_control WHERE id),9223372036854775807);
CREATE VIEW exec_visible_receipts AS
 SELECT r.* FROM receipts r WHERE r.block_number <= COALESCE(
  (SELECT visible_height FROM exec_control WHERE id),9223372036854775807);
CREATE VIEW exec_visible_logs AS
 SELECT l.* FROM logs l WHERE l.block_number <= COALESCE(
  (SELECT visible_height FROM exec_control WHERE id),9223372036854775807);
CREATE VIEW exec_public_meta AS
 SELECT name,CASE WHEN name='latest_height' THEN COALESCE(
  (SELECT int8send(visible_height) FROM exec_control WHERE id),value) ELSE value END AS value
 FROM meta;

CREATE TABLE exec_rewinds (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 request_generation bigint NOT NULL CHECK(request_generation>0),
 from_generation bigint NOT NULL CHECK(from_generation>0),
 from_height bigint NOT NULL CHECK(from_height>=0),
 from_hash bytea NOT NULL CHECK(octet_length(from_hash)=32),
 from_root bytea NOT NULL CHECK(octet_length(from_root)=32),
 generation bigint NOT NULL CHECK(generation>request_generation),
 target_height bigint NOT NULL CHECK(target_height>=0 AND target_height<=from_height),
 target_hash bytea NOT NULL CHECK(octet_length(target_hash)=32),
 target_root bytea NOT NULL CHECK(octet_length(target_root)=32)
);
CREATE INDEX state_accounts_last_height ON state_accounts(last_height);
CREATE INDEX state_storage_last_height ON state_storage(last_height);
CREATE INDEX state_storage_roots_last_height ON state_storage_roots(last_height);
CREATE INDEX state_contracts_last_height ON state_contracts(last_height);
