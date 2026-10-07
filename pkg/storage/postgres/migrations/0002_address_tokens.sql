-- Address index, token holders and token metadata (refactoring plan R4-2,
-- stage 2). Amounts and token ids are numeric, so they keep every digit and
-- sort by value.

CREATE TABLE contract_creations (
    contract_address bytea   PRIMARY KEY,
    creator          bytea   NOT NULL,
    tx_hash          bytea   NOT NULL,
    block_number     bigint  NOT NULL,
    timestamp        bigint  NOT NULL,
    bytecode_size    integer NOT NULL
);
CREATE INDEX contract_creations_creator ON contract_creations (creator, block_number, contract_address);
CREATE INDEX contract_creations_block ON contract_creations (block_number, contract_address);

CREATE TABLE internal_transactions (
    tx_hash      bytea   NOT NULL,
    idx          integer NOT NULL,
    block_number bigint  NOT NULL,
    type         text    NOT NULL,
    from_addr    bytea   NOT NULL,
    to_addr      bytea   NOT NULL,
    value        numeric NOT NULL,
    gas          bigint  NOT NULL,
    gas_used     bigint  NOT NULL,
    input        bytea   NOT NULL,
    output       bytea   NOT NULL,
    error        text    NOT NULL,
    depth        integer NOT NULL,
    PRIMARY KEY (tx_hash, idx)
);
CREATE INDEX internal_transactions_from ON internal_transactions (from_addr, block_number, tx_hash, idx);
CREATE INDEX internal_transactions_to ON internal_transactions (to_addr, block_number, tx_hash, idx);

CREATE TABLE erc20_transfers (
    tx_hash      bytea   NOT NULL,
    log_index    integer NOT NULL,
    contract     bytea   NOT NULL,
    from_addr    bytea   NOT NULL,
    to_addr      bytea   NOT NULL,
    value        numeric NOT NULL,
    block_number bigint  NOT NULL,
    timestamp    bigint  NOT NULL,
    PRIMARY KEY (tx_hash, log_index)
);
CREATE INDEX erc20_transfers_contract ON erc20_transfers (contract, block_number, log_index, tx_hash);
CREATE INDEX erc20_transfers_from ON erc20_transfers (from_addr, block_number, log_index, tx_hash);
CREATE INDEX erc20_transfers_to ON erc20_transfers (to_addr, block_number, log_index, tx_hash);

CREATE TABLE erc721_transfers (
    tx_hash      bytea   NOT NULL,
    log_index    integer NOT NULL,
    contract     bytea   NOT NULL,
    from_addr    bytea   NOT NULL,
    to_addr      bytea   NOT NULL,
    token_id     numeric NOT NULL,
    block_number bigint  NOT NULL,
    timestamp    bigint  NOT NULL,
    PRIMARY KEY (tx_hash, log_index)
);
CREATE INDEX erc721_transfers_contract ON erc721_transfers (contract, block_number, log_index, tx_hash);
CREATE INDEX erc721_transfers_from ON erc721_transfers (from_addr, block_number, log_index, tx_hash);
CREATE INDEX erc721_transfers_to ON erc721_transfers (to_addr, block_number, log_index, tx_hash);

-- The current owner of each NFT; a burned token keeps the zero address.
CREATE TABLE nft_owners (
    contract bytea   NOT NULL,
    token_id numeric NOT NULL,
    owner    bytea   NOT NULL,
    PRIMARY KEY (contract, token_id)
);
CREATE INDEX nft_owners_owner ON nft_owners (owner, contract, token_id);

CREATE TABLE token_holders (
    token           bytea   NOT NULL,
    holder          bytea   NOT NULL,
    balance         numeric NOT NULL,
    last_updated_at bigint  NOT NULL,
    PRIMARY KEY (token, holder)
);
CREATE INDEX token_holders_balance ON token_holders (token, balance DESC, holder);
CREATE INDEX token_holders_holder ON token_holders (holder, token);

CREATE TABLE token_holder_stats (
    token            bytea   PRIMARY KEY,
    holder_count     integer NOT NULL,
    transfer_count   integer NOT NULL,
    last_activity_at bigint  NOT NULL
);

-- Times are Unix nanoseconds: timestamptz keeps microseconds only.
CREATE TABLE token_metadata (
    address             bytea    PRIMARY KEY,
    standard            text     NOT NULL,
    name                text     NOT NULL,
    symbol              text     NOT NULL,
    decimals            smallint NOT NULL,
    total_supply        numeric,
    base_uri            text     NOT NULL,
    detected_at         bigint   NOT NULL,
    created_at          bigint   NOT NULL,
    updated_at          bigint   NOT NULL,
    supports_erc165     boolean  NOT NULL,
    supports_metadata   boolean  NOT NULL,
    supports_enumerable boolean  NOT NULL
);
CREATE INDEX token_metadata_standard ON token_metadata (standard, address);
