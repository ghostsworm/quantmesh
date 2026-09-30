-- estimated: <1s. Gives each process instance an independent durable pause owner.
CREATE TABLE IF NOT EXISTS opening_pause_owners (
    owner_id VARCHAR(64) NOT NULL,
    source VARCHAR(96) NOT NULL,
    reason TEXT NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (owner_id, source),
    INDEX idx_opening_pause_owners_source (source)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
