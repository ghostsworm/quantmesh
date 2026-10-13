CREATE TABLE IF NOT EXISTS funding_recovery_episodes (
    episode_id VARCHAR(64) NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    recovery_generation BIGINT UNSIGNED NOT NULL,
    state VARCHAR(32) NOT NULL,
    bot_id VARCHAR(191) NOT NULL,
    strategy VARCHAR(191) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id),
    INDEX idx_funding_recovery_state (state, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_adjudications (
    episode_id VARCHAR(64) NOT NULL,
    operation_id VARCHAR(191) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    permit_id VARCHAR(191) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id, operation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_children (
    episode_id VARCHAR(64) NOT NULL,
    child_id VARCHAR(191) NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    payload_json JSON NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id, child_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_actions (
    episode_id VARCHAR(64) NOT NULL,
    action_id VARCHAR(191) NOT NULL,
    ordinal BIGINT UNSIGNED NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    payload_json JSON NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id, action_id),
    UNIQUE KEY uk_funding_recovery_action_ordinal (episode_id, ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_events (
    event_key CHAR(64) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    event_id VARCHAR(191) NOT NULL,
    event_revision BIGINT UNSIGNED NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (event_key),
    UNIQUE KEY uk_funding_recovery_event_id (episode_id, event_id),
    INDEX idx_funding_recovery_events_episode (episode_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_operations (
    episode_id VARCHAR(64) NOT NULL,
    operation_id VARCHAR(191) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    phase VARCHAR(32) NOT NULL,
    receipt_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id, operation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_outbox (
    episode_id VARCHAR(64) NOT NULL,
    operation_id VARCHAR(191) NOT NULL,
    state VARCHAR(32) NOT NULL,
    target_episode_revision BIGINT UNSIGNED NOT NULL,
    projection_hash CHAR(64) NOT NULL,
    payload_json JSON NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (episode_id, operation_id),
    INDEX idx_funding_recovery_outbox_state (state, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_holds (
    hold_id VARCHAR(191) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    recovery_generation BIGINT UNSIGNED NOT NULL,
    hold_revision BIGINT UNSIGNED NOT NULL,
    state VARCHAR(32) NOT NULL,
    payload_json JSON NOT NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (hold_id),
    UNIQUE KEY uk_funding_recovery_hold_generation (episode_id, recovery_generation),
    INDEX idx_funding_recovery_holds_state (state, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_permits (
    permit_id VARCHAR(191) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    recovery_generation BIGINT UNSIGNED NOT NULL,
    action_id VARCHAR(191) NOT NULL,
    lease_id BIGINT UNSIGNED NOT NULL,
    state VARCHAR(32) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    consumed_at DATETIME(3) NULL,
    PRIMARY KEY (permit_id),
    UNIQUE KEY uk_funding_recovery_permit_action (episode_id, recovery_generation, action_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS funding_recovery_submission_attempts (
    permit_id VARCHAR(191) NOT NULL,
    episode_id VARCHAR(64) NOT NULL,
    recovery_generation BIGINT UNSIGNED NOT NULL,
    action_id VARCHAR(191) NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    state VARCHAR(40) NOT NULL,
    client_order_id VARCHAR(191) NOT NULL,
    intent_hash CHAR(64) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (permit_id),
    UNIQUE KEY uk_funding_recovery_attempt_action (episode_id, recovery_generation, action_id),
    INDEX idx_funding_recovery_attempt_state (episode_id, state, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
