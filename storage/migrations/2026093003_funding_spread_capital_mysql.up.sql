-- estimated: <1s. Existing claims receive an empty legacy generation and cannot be released by an unverified stale runtime.
ALTER TABLE funding_spread_capital_reservations
    ADD COLUMN reservation_token CHAR(64) NOT NULL DEFAULT '' AFTER bot_key;
