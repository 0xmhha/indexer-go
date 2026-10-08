-- Contract ABIs and source verification (refactoring plan R4-2, stage 4).

CREATE TABLE abis (
    address bytea PRIMARY KEY,
    abi     bytea NOT NULL
);

-- verified_at is Unix seconds, the list order of the Pebble store; data is
-- the port record as JSON.
CREATE TABLE contract_verifications (
    address     bytea   PRIMARY KEY,
    is_verified boolean NOT NULL,
    verified_at bigint  NOT NULL,
    data        bytea   NOT NULL
);
CREATE INDEX contract_verifications_verified ON contract_verifications (verified_at, address) WHERE is_verified;
