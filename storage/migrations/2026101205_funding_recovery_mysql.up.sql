CREATE TABLE IF NOT EXISTS funding_recovery_source_anomalies (
    anomaly_id CHAR(64) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    source_identity_hash CHAR(64) NOT NULL,
    cursor_revision BIGINT UNSIGNED NOT NULL,
    observed_history_hash CHAR(64) NOT NULL,
    state VARCHAR(32) NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (anomaly_id),
    UNIQUE KEY uk_funding_recovery_source_anomalies_scan (episode_id, source_identity_hash, cursor_revision, observed_history_hash),
    KEY idx_funding_recovery_source_anomalies_episode_state (episode_id, state, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
