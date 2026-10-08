-- Account abstraction: EIP-7702 SetCode authorizations, ERC-4337
-- UserOperations and ERC-7579 modules (refactoring plan R4-2, stage 3).
-- data holds the port record as JSON, as the Pebble store keeps it; the
-- other columns are what the lists filter and sort on.

CREATE TABLE setcode_authorizations (
    tx_hash      bytea   NOT NULL,
    auth_index   integer NOT NULL,
    block_number bigint  NOT NULL,
    tx_index     bigint  NOT NULL,
    target       bytea   NOT NULL,
    authority    bytea   NOT NULL,
    data         bytea   NOT NULL,
    PRIMARY KEY (tx_hash, auth_index)
);
CREATE INDEX setcode_authorizations_target ON setcode_authorizations (target, block_number, tx_index, auth_index);
CREATE INDEX setcode_authorizations_authority ON setcode_authorizations (authority, block_number, tx_index, auth_index);
CREATE INDEX setcode_authorizations_block ON setcode_authorizations (block_number, tx_index, auth_index);

CREATE TABLE setcode_delegations (
    address bytea PRIMARY KEY,
    data    bytea NOT NULL
);

-- last_activity_time is Unix nanoseconds.
CREATE TABLE setcode_stats (
    address             bytea   PRIMARY KEY,
    as_target_count     integer NOT NULL,
    as_authority_count  integer NOT NULL,
    last_activity_block bigint  NOT NULL,
    last_activity_time  bigint  NOT NULL
);

-- paymaster and factory are NULL when the operation has none (or the zero
-- address), so it is not listed under them.
CREATE TABLE user_operations (
    hash         bytea  PRIMARY KEY,
    sender       bytea  NOT NULL,
    bundler      bytea  NOT NULL,
    paymaster    bytea,
    factory      bytea,
    block_number bigint NOT NULL,
    tx_hash      bytea  NOT NULL,
    bundle_index bigint NOT NULL,
    data         bytea  NOT NULL
);
CREATE INDEX user_operations_sender ON user_operations (sender, block_number, tx_hash, bundle_index);
CREATE INDEX user_operations_bundler ON user_operations (bundler, block_number, tx_hash, bundle_index);
CREATE INDEX user_operations_paymaster ON user_operations (paymaster, block_number, tx_hash, bundle_index) WHERE paymaster IS NOT NULL;
CREATE INDEX user_operations_factory ON user_operations (factory, block_number, tx_hash, bundle_index) WHERE factory IS NOT NULL;
CREATE INDEX user_operations_block ON user_operations (block_number, tx_hash, bundle_index);
CREATE INDEX user_operations_tx ON user_operations (tx_hash, bundle_index);

CREATE TABLE bundler_stats (
    address bytea PRIMARY KEY,
    data    bytea NOT NULL
);

CREATE TABLE factory_stats (
    address bytea PRIMARY KEY,
    data    bytea NOT NULL
);

CREATE TABLE paymaster_stats (
    address bytea PRIMARY KEY,
    data    bytea NOT NULL
);

CREATE TABLE smart_accounts (
    address bytea PRIMARY KEY,
    data    bytea NOT NULL
);

-- One install record per account and module; a reinstall replaces it.
CREATE TABLE installed_modules (
    account      bytea    NOT NULL,
    module       bytea    NOT NULL,
    module_type  smallint NOT NULL,
    installed_at bigint   NOT NULL,
    data         bytea    NOT NULL,
    PRIMARY KEY (account, module)
);
CREATE INDEX installed_modules_account ON installed_modules (account, installed_at, module);
CREATE INDEX installed_modules_type ON installed_modules (module_type, installed_at, account, module);
CREATE INDEX installed_modules_block ON installed_modules (installed_at, account, module);

CREATE TABLE module_stats (
    module bytea PRIMARY KEY,
    data   bytea NOT NULL
);
