-- estimated: <1s on a new table. No existing account or order data is modified.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_locks (
    wallet_key TEXT NOT NULL PRIMARY KEY,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS funding_spread_capital_reservations (
    wallet_key TEXT NOT NULL,
    bot_key TEXT NOT NULL,
    amount NUMERIC NOT NULL CHECK (amount > 0),
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (wallet_key, bot_key)
);
