CREATE TABLE IF NOT EXISTS funding_carry_runtime_state_write_receipts (
    bot_id VARCHAR(255) NOT NULL,
    strategy_name VARCHAR(128) NOT NULL,
    write_id CHAR(64) NOT NULL,
    owner_token CHAR(64) NOT NULL,
    PRIMARY KEY (bot_id, strategy_name),
    UNIQUE KEY uk_funding_carry_runtime_state_write_id (write_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
