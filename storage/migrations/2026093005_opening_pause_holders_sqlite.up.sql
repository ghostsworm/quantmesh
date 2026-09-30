-- estimated: <1s. Gives each process instance an independent durable pause owner.
CREATE TABLE IF NOT EXISTS opening_pause_owners (
    owner_id TEXT NOT NULL,
    source TEXT NOT NULL CHECK (length(source) <= 96),
    reason TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (owner_id, source)
);
CREATE INDEX IF NOT EXISTS idx_opening_pause_owners_source ON opening_pause_owners (source);
