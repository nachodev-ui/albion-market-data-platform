BEGIN;

CREATE TABLE IF NOT EXISTS normalized_market_history (
    id BIGSERIAL PRIMARY KEY,
    dedupe_key CHAR(64) NOT NULL UNIQUE,
    schema_version SMALLINT NOT NULL DEFAULT 1,
    source VARCHAR(64) NOT NULL,
    server VARCHAR(16) NOT NULL,
    albion_id INTEGER NOT NULL,
    item_id VARCHAR(160) NOT NULL,
    item_name VARCHAR(240),
    location_id VARCHAR(80) NOT NULL,
    location_name VARCHAR(160),
    quality SMALLINT NOT NULL CHECK (quality BETWEEN 1 AND 5),
    period VARCHAR(16) NOT NULL,
    sold_units BIGINT NOT NULL CHECK (sold_units >= 0),
    active_buckets INTEGER NOT NULL CHECK (active_buckets >= 0),
    total_silver BIGINT NOT NULL CHECK (total_silver >= 0),
    weighted_average_unit_price NUMERIC(24, 6) NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL,
    history JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_normalized_history_lookup
    ON normalized_market_history (server, item_id, location_id, quality, period, captured_at DESC);

CREATE TABLE IF NOT EXISTS normalized_market_order_snapshot (
    id BIGSERIAL PRIMARY KEY,
    dedupe_key CHAR(64) NOT NULL UNIQUE,
    schema_version SMALLINT NOT NULL DEFAULT 1,
    source VARCHAR(64) NOT NULL,
    server VARCHAR(16) NOT NULL,
    order_id BIGINT NOT NULL,
    albion_id INTEGER,
    item_id VARCHAR(160) NOT NULL,
    item_name VARCHAR(240),
    item_group_id VARCHAR(160),
    enchantment_level SMALLINT NOT NULL CHECK (enchantment_level BETWEEN 0 AND 4),
    location_id VARCHAR(80) NOT NULL,
    location_name VARCHAR(160),
    quality SMALLINT NOT NULL CHECK (quality BETWEEN 1 AND 5),
    auction_type VARCHAR(16) NOT NULL,
    side VARCHAR(16) NOT NULL,
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    amount BIGINT NOT NULL CHECK (amount >= 0),
    expires_at TIMESTAMPTZ NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_normalized_order_lookup
    ON normalized_market_order_snapshot (server, item_id, location_id, quality, side, captured_at DESC);

CREATE INDEX IF NOT EXISTS idx_normalized_order_latest
    ON normalized_market_order_snapshot (server, order_id, captured_at DESC);

COMMIT;
