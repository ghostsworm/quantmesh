CREATE TABLE IF NOT EXISTS funding_recovery_source_cursors (
    cursor_key TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    source TEXT NOT NULL,
    exchange_name TEXT NOT NULL,
    market TEXT NOT NULL,
    account_scope TEXT NOT NULL,
    event_type TEXT NOT NULL,
    asset TEXT NOT NULL,
    window_from TIMESTAMP NOT NULL,
    window_through TIMESTAMP NOT NULL,
    complete_rows INTEGER NOT NULL,
    total_rows INTEGER NOT NULL,
    completion_marker TEXT NOT NULL,
    evidence_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_source_cursors_episode ON funding_recovery_source_cursors(episode_id);
