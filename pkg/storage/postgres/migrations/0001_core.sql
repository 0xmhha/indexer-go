-- Core chain data (refactoring plan R4-2, stage 1): blocks, transactions,
-- receipts and logs, the indexed-height cursor, the address transaction
-- list, feature states and the key-value space of chain packages.
--
-- Each row keeps the chain-neutral model encoded with pkg/core/model
-- (data) next to the columns queries filter and sort on, so a row reads
-- back exactly as the chain profile decoded it.

CREATE TABLE meta (
    name  text PRIMARY KEY,
    value bytea NOT NULL
);

CREATE TABLE blocks (
    number      bigint PRIMARY KEY,
    hash        bytea  NOT NULL,
    parent_hash bytea  NOT NULL,
    time        bigint NOT NULL,
    miner       bytea  NOT NULL,
    data        bytea  NOT NULL
);
CREATE INDEX blocks_hash ON blocks (hash);
CREATE INDEX blocks_time ON blocks (time, number);

CREATE TABLE transactions (
    hash         bytea   PRIMARY KEY,
    block_number bigint  NOT NULL,
    tx_index     integer NOT NULL,
    block_hash   bytea   NOT NULL,
    from_addr    bytea   NOT NULL,
    to_addr      bytea,
    data         bytea   NOT NULL
);
CREATE INDEX transactions_block ON transactions (block_number, tx_index);

CREATE TABLE receipts (
    tx_hash          bytea   PRIMARY KEY,
    block_number     bigint  NOT NULL,
    tx_index         integer NOT NULL,
    block_hash       bytea   NOT NULL,
    status           bigint  NOT NULL,
    contract_address bytea,
    data             bytea   NOT NULL
);
CREATE INDEX receipts_block ON receipts (block_number, tx_index);
CREATE INDEX receipts_block_hash ON receipts (block_hash);

CREATE TABLE logs (
    block_number bigint  NOT NULL,
    tx_index     integer NOT NULL,
    log_index    integer NOT NULL,
    block_hash   bytea   NOT NULL,
    tx_hash      bytea   NOT NULL,
    address      bytea   NOT NULL,
    topic0       bytea,
    topic1       bytea,
    topic2       bytea,
    topic3       bytea,
    topics       bytea   NOT NULL, -- every topic, 32 bytes each
    data         bytea   NOT NULL,
    removed      boolean NOT NULL,
    PRIMARY KEY (block_number, tx_index, log_index)
);
CREATE INDEX logs_address ON logs (address, block_number, tx_index, log_index);
CREATE INDEX logs_topic0 ON logs (topic0, block_number, tx_index, log_index);
CREATE INDEX logs_topic1 ON logs (topic1, block_number, tx_index, log_index);
CREATE INDEX logs_topic2 ON logs (topic2, block_number, tx_index, log_index);
CREATE INDEX logs_topic3 ON logs (topic3, block_number, tx_index, log_index);

-- The transactions of an address in the order they were indexed.
CREATE TABLE address_transactions (
    id      bigserial PRIMARY KEY,
    address bytea     NOT NULL,
    tx_hash bytea     NOT NULL
);
CREATE INDEX address_transactions_list ON address_transactions (address, id);

CREATE TABLE feature_states (
    name  text  PRIMARY KEY,
    state jsonb NOT NULL
);

-- port.KV: data of chain packages and other owners of a key prefix.
CREATE TABLE kv (
    key   bytea PRIMARY KEY,
    value bytea NOT NULL
);
