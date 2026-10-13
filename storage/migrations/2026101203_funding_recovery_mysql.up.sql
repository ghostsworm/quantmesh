CREATE TABLE IF NOT EXISTS funding_recovery_origins (
    origin_key CHAR(64) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (origin_key),
    UNIQUE KEY uk_funding_recovery_origin_episode (episode_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
