CREATE TABLE IF NOT EXISTS funding_recovery_origins (
    origin_key TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
