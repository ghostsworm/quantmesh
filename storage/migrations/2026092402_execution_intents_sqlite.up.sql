-- estimated: <1s on a new table; no existing order data is modified.
CREATE TABLE IF NOT EXISTS execution_intents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_key TEXT NOT NULL,
    client_order_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    state_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uk_execution_intents_scope_cid UNIQUE (scope_key, client_order_id)
);
CREATE INDEX IF NOT EXISTS idx_execution_intents_scope_id ON execution_intents (scope_key, id);
