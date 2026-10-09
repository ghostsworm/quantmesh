CREATE TABLE IF NOT EXISTS funding_carry_runtime_state_write_receipts (
    bot_id TEXT NOT NULL,
    strategy_name TEXT NOT NULL,
    write_id TEXT NOT NULL UNIQUE,
    owner_token TEXT NOT NULL,
    PRIMARY KEY (bot_id, strategy_name)
);
