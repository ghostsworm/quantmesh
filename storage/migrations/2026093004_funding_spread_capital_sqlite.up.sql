-- estimated: <1s on a new table. Existing reservations remain unchanged.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_balances (
    wallet_key TEXT NOT NULL PRIMARY KEY,
    available NUMERIC NOT NULL CHECK (available > 0),
    observed_at_ns INTEGER NOT NULL CHECK (observed_at_ns > 0)
);
