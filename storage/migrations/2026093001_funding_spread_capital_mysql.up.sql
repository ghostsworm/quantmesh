-- estimated: <1s on a new table. No existing account or order data is modified.
CREATE TABLE IF NOT EXISTS funding_spread_wallet_locks (
    wallet_key CHAR(64) NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (wallet_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS funding_spread_capital_reservations (
    wallet_key CHAR(64) NOT NULL,
    bot_key CHAR(64) NOT NULL,
    amount DECIMAL(30,12) NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (wallet_key, bot_key),
    CONSTRAINT chk_funding_spread_reservation_amount CHECK (amount > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
