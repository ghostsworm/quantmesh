-- estimated: <1s on a new table. Existing wallet balances and reservations remain unchanged.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_observation_sequences (
    wallet_key TEXT NOT NULL PRIMARY KEY,
    latest_sequence INTEGER NOT NULL DEFAULT 0 CHECK (latest_sequence >= 0),
    available NUMERIC NULL CHECK (available IS NULL OR available > 0),
    observed_at_ns INTEGER NULL CHECK (observed_at_ns IS NULL OR observed_at_ns > 0)
);
