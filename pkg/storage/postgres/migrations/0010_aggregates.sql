-- Candles of DEX markets and time series (refactoring plan R5-3). data holds
-- the port record as JSON, as the Pebble store keeps it.

CREATE TABLE agg_candles (
    address   bytea  NOT NULL,
    market_id bigint NOT NULL,
    seconds   bigint NOT NULL,
    start     bigint NOT NULL,
    data      bytea  NOT NULL,
    PRIMARY KEY (address, market_id, seconds, start)
);

CREATE TABLE agg_series (
    series  text   NOT NULL,
    subject text   NOT NULL,
    period  text   NOT NULL,
    start   bigint NOT NULL,
    data    bytea  NOT NULL,
    PRIMARY KEY (series, subject, period, start)
);

SELECT undo_track(t) FROM unnest(ARRAY['agg_candles', 'agg_series']) AS t;
