-- DEX markets, trades, liquidity changes and perpetual orders (refactoring
-- plan R5-1). data holds the port record as JSON, as the Pebble store keeps
-- it; the other columns are what the lists filter and sort on. A trade or a
-- liquidity change is identified by its log.

CREATE TABLE dex_markets (
    address           bytea  NOT NULL,
    market_id         bigint NOT NULL,
    created_block     bigint NOT NULL,
    created_log_index bigint NOT NULL,
    data              bytea  NOT NULL,
    PRIMARY KEY (address, market_id)
);
CREATE INDEX dex_markets_created ON dex_markets (created_block, created_log_index);

-- maker is NULL when the trade has none (or the zero address).
CREATE TABLE dex_trades (
    block_number bigint NOT NULL,
    log_index    bigint NOT NULL,
    address      bytea  NOT NULL,
    market_id    bigint NOT NULL,
    taker        bytea,
    maker        bytea,
    data         bytea  NOT NULL,
    PRIMARY KEY (block_number, log_index)
);
CREATE INDEX dex_trades_market ON dex_trades (address, market_id, block_number, log_index);
CREATE INDEX dex_trades_taker ON dex_trades (taker, block_number, log_index) WHERE taker IS NOT NULL;
CREATE INDEX dex_trades_maker ON dex_trades (maker, block_number, log_index) WHERE maker IS NOT NULL;

CREATE TABLE dex_liquidity (
    block_number bigint NOT NULL,
    log_index    bigint NOT NULL,
    address      bytea  NOT NULL,
    market_id    bigint NOT NULL,
    data         bytea  NOT NULL,
    PRIMARY KEY (block_number, log_index)
);
CREATE INDEX dex_liquidity_market ON dex_liquidity (address, market_id, block_number, log_index);

CREATE TABLE dex_orders (
    manager           bytea  NOT NULL,
    order_id          bytea  NOT NULL,
    market_id         bigint NOT NULL,
    created_block     bigint NOT NULL,
    created_log_index bigint NOT NULL,
    data              bytea  NOT NULL,
    PRIMARY KEY (manager, order_id)
);
CREATE INDEX dex_orders_market ON dex_orders (manager, market_id, created_block, created_log_index);

SELECT undo_track(t) FROM unnest(ARRAY['dex_markets', 'dex_trades', 'dex_liquidity', 'dex_orders']) AS t;
