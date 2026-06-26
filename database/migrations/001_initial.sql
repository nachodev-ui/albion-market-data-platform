BEGIN;

CREATE TABLE IF NOT EXISTS market_history_raw_event (
    id BIGSERIAL PRIMARY KEY,
    collector_id UUID NOT NULL,
    server VARCHAR(16) NOT NULL,
    source VARCHAR(64) NOT NULL,
    item_id VARCHAR(160),
    albion_id INTEGER NOT NULL,
    location_id VARCHAR(80) NOT NULL,
    quality SMALLINT NOT NULL CHECK (quality BETWEEN 1 AND 5),
    timescale SMALLINT NOT NULL CHECK (timescale BETWEEN 0 AND 2),
    captured_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_market_history_raw_lookup
    ON market_history_raw_event (server, item_id, location_id, quality, captured_at DESC);

CREATE TABLE IF NOT EXISTS market_history_observation (
    id BIGSERIAL PRIMARY KEY,
    raw_event_id BIGINT NOT NULL REFERENCES market_history_raw_event(id) ON DELETE CASCADE,
    server VARCHAR(16) NOT NULL,
    item_id VARCHAR(160) NOT NULL,
    albion_id INTEGER NOT NULL,
    location_id VARCHAR(80) NOT NULL,
    quality SMALLINT NOT NULL CHECK (quality BETWEEN 1 AND 5),
    timescale SMALLINT NOT NULL CHECK (timescale BETWEEN 0 AND 2),
    bucket_start TIMESTAMPTZ NOT NULL,
    item_count BIGINT NOT NULL CHECK (item_count >= 0),
    silver_amount NUMERIC(24, 0) NOT NULL CHECK (silver_amount >= 0),
    captured_at TIMESTAMPTZ NOT NULL,
    UNIQUE (server, item_id, location_id, quality, timescale, bucket_start, captured_at)
);

CREATE INDEX IF NOT EXISTS idx_market_history_observation_lookup
    ON market_history_observation (server, item_id, location_id, quality, bucket_start DESC);

COMMIT;
