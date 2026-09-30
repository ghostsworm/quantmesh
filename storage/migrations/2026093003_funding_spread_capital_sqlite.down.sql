-- Rollback only after all updated runtimes have stopped; removing generation tokens re-enables stale-owner deletion races.
ALTER TABLE funding_spread_capital_reservations DROP COLUMN reservation_token;
