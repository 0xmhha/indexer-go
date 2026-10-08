-- Native balance history (refactoring plan R4-2, stage 4). Every update adds
-- a snapshot in write order (id); balances holds the latest balance of
-- each account that has one.

CREATE TABLE balance_history (
    id           bigserial PRIMARY KEY,
    address      bytea     NOT NULL,
    block_number bigint    NOT NULL,
    balance      numeric   NOT NULL,
    delta        numeric   NOT NULL,
    tx_hash      bytea     NOT NULL
);
CREATE INDEX balance_history_address ON balance_history (address, id);

CREATE TABLE balances (
    address bytea   PRIMARY KEY,
    balance numeric NOT NULL
);
