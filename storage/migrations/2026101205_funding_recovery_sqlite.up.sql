CREATE TABLE IF NOT EXISTS funding_recovery_source_anomalies (
    anomaly_id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    source_identity_hash TEXT NOT NULL,
    cursor_revision INTEGER NOT NULL,
    observed_history_hash TEXT NOT NULL,
    state TEXT NOT NULL,
    revision INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_funding_recovery_source_anomalies_scan
    ON funding_recovery_source_anomalies(episode_id, source_identity_hash, cursor_revision, observed_history_hash);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_source_anomalies_episode_state
    ON funding_recovery_source_anomalies(episode_id, state, created_at);
