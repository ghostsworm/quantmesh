-- estimated: <1s. Persists global risk pause ownership across process restarts.
CREATE TABLE IF NOT EXISTS opening_pause_holders (
    source VARCHAR(128) NOT NULL PRIMARY KEY,
    reason TEXT NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS opening_pause_state_migrations (
    migration_id VARCHAR(128) NOT NULL PRIMARY KEY,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
