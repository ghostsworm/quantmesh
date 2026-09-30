-- estimated: <1s. Persists global risk pause ownership across process restarts.
CREATE TABLE IF NOT EXISTS opening_pause_holders (
    source TEXT PRIMARY KEY,
    reason TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS opening_pause_state_migrations (
    migration_id TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
