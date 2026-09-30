-- Rollback only after all funding_perp_spread runtimes have stopped safely.
DROP TABLE IF EXISTS funding_spread_capital_reservations;
DROP TABLE IF EXISTS funding_spread_wallet_locks;
