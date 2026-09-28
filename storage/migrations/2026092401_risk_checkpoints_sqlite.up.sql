-- estimated: <1s on an empty new table; no existing trading data is modified.
CREATE TABLE IF NOT EXISTS risk_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    checkpoint_key TEXT NOT NULL UNIQUE,
    revision INTEGER NOT NULL,
    state_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
