-- estimated: <1s on a new table. Existing reservations remain unchanged.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_balances (
    wallet_key CHAR(64) NOT NULL,
    available DECIMAL(30,12) NOT NULL,
    observed_at_ns BIGINT NOT NULL,
    PRIMARY KEY (wallet_key),
    CONSTRAINT chk_funding_spread_wallet_available CHECK (available > 0),
    CONSTRAINT chk_funding_spread_wallet_observed_at CHECK (observed_at_ns > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
