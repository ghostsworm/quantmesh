CREATE TABLE IF NOT EXISTS funding_recovery_episodes (
    episode_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    recovery_generation INTEGER NOT NULL,
    state TEXT NOT NULL,
    bot_id TEXT NOT NULL,
    strategy TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_state ON funding_recovery_episodes(state, updated_at);
CREATE TABLE IF NOT EXISTS funding_recovery_children (
    episode_id TEXT NOT NULL,
    child_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, child_id)
);
CREATE TABLE IF NOT EXISTS funding_recovery_actions (
    episode_id TEXT NOT NULL,
    action_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, action_id),
    UNIQUE (episode_id, ordinal)
);
CREATE TABLE IF NOT EXISTS funding_recovery_events (
    event_key TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    event_revision INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (episode_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_events_episode ON funding_recovery_events(episode_id, created_at);
CREATE TABLE IF NOT EXISTS funding_recovery_operations (
    episode_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    phase TEXT NOT NULL,
    receipt_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, operation_id)
);
CREATE TABLE IF NOT EXISTS funding_recovery_outbox (
    episode_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    state TEXT NOT NULL,
    target_episode_revision INTEGER NOT NULL,
    projection_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, operation_id)
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_outbox_state ON funding_recovery_outbox(state, updated_at);
CREATE TABLE IF NOT EXISTS funding_recovery_holds (
    hold_id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    recovery_generation INTEGER NOT NULL,
    hold_revision INTEGER NOT NULL,
    state TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (episode_id, recovery_generation)
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_holds_state ON funding_recovery_holds(state, updated_at);
CREATE TABLE IF NOT EXISTS funding_recovery_permits (
    permit_id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    recovery_generation INTEGER NOT NULL,
    action_id TEXT NOT NULL,
    lease_id INTEGER NOT NULL,
    state TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    consumed_at TIMESTAMP NULL,
    UNIQUE (episode_id, recovery_generation, action_id)
);
CREATE TABLE IF NOT EXISTS funding_recovery_submission_attempts (
    permit_id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL,
    recovery_generation INTEGER NOT NULL,
    action_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    state TEXT NOT NULL,
    client_order_id TEXT NOT NULL,
    intent_hash TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (episode_id, recovery_generation, action_id)
);
CREATE INDEX IF NOT EXISTS idx_funding_recovery_attempts_state ON funding_recovery_submission_attempts(episode_id, state, updated_at);
CREATE TABLE IF NOT EXISTS funding_recovery_adjudications (
    episode_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    permit_id TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, operation_id)
);
