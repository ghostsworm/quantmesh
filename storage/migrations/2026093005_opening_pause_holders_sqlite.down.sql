-- Preserve every active owner as an operator-visible fail-closed legacy gate.
INSERT OR IGNORE INTO opening_pause_holders (source, reason)
SELECT substr(source, 1, 80) || ':down:' || owner_id, reason
FROM opening_pause_owners;
DELETE FROM opening_pause_state_migrations WHERE migration_id = '2026093005';
DROP TABLE IF EXISTS opening_pause_owners;
