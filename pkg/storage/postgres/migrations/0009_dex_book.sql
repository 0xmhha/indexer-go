-- Order book state (refactoring plan R5-2): the initialized ticks of Uniswap
-- V3 pools and the resting orders (open or partially filled) of perpetual
-- markets. A tick row is removed when its gross liquidity returns to zero,
-- an open order row when the order stops resting.

CREATE TABLE dex_ticks (
    address   bytea   NOT NULL,
    market_id bigint  NOT NULL,
    tick      integer NOT NULL,
    data      bytea   NOT NULL,
    PRIMARY KEY (address, market_id, tick)
);

CREATE TABLE dex_open_orders (
    manager           bytea  NOT NULL,
    order_id          bytea  NOT NULL,
    market_id         bigint NOT NULL,
    created_block     bigint NOT NULL,
    created_log_index bigint NOT NULL,
    PRIMARY KEY (manager, order_id)
);
CREATE INDEX dex_open_orders_market ON dex_open_orders (manager, market_id, created_block, created_log_index);

-- Orders indexed before this migration.
INSERT INTO dex_open_orders (manager, order_id, market_id, created_block, created_log_index)
SELECT manager, order_id, market_id, created_block, created_log_index FROM dex_orders
WHERE convert_from(data, 'UTF8')::jsonb->>'status' IN ('open', 'partially_filled');

SELECT undo_track(t) FROM unnest(ARRAY['dex_ticks', 'dex_open_orders']) AS t;
