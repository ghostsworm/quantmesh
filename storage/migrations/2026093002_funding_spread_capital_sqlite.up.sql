-- estimated: <1s. Stores only non-credential identifiers for reservation audits.
CREATE TABLE IF NOT EXISTS funding_spread_capital_reservation_metadata (
    wallet_key TEXT NOT NULL,
    bot_key TEXT NOT NULL,
    exchange_name TEXT NOT NULL,
    market_type TEXT NOT NULL,
    quote_asset TEXT NOT NULL,
    symbol TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (wallet_key, bot_key)
);
