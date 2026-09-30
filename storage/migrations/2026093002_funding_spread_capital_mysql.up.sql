-- estimated: <1s. Stores only non-credential identifiers for reservation audits.
CREATE TABLE IF NOT EXISTS funding_spread_capital_reservation_metadata (
    wallet_key CHAR(64) NOT NULL,
    bot_key CHAR(64) NOT NULL,
    exchange_name VARCHAR(128) NOT NULL,
    market_type VARCHAR(128) NOT NULL,
    quote_asset VARCHAR(128) NOT NULL,
    symbol VARCHAR(128) NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (wallet_key, bot_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
