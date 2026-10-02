-- estimated: <1s on a new table. Existing wallet balances and reservations remain unchanged.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_observation_sequences (
    wallet_key CHAR(64) NOT NULL,
    latest_sequence BIGINT NOT NULL DEFAULT 0,
    available DECIMAL(30,12) NULL,
    observed_at_ns BIGINT NULL,
    PRIMARY KEY (wallet_key),
    CONSTRAINT chk_funding_spread_wallet_sequence CHECK (latest_sequence >= 0),
    CONSTRAINT chk_funding_spread_sequence_available CHECK (available IS NULL OR available > 0),
    CONSTRAINT chk_funding_spread_sequence_observed CHECK (observed_at_ns IS NULL OR observed_at_ns > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
