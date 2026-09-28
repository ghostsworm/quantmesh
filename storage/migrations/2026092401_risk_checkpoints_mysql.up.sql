-- estimated: <1s on an empty new table; no existing trading data is modified.
CREATE TABLE IF NOT EXISTS risk_checkpoints (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    checkpoint_key VARCHAR(191) NOT NULL,
    revision BIGINT NOT NULL,
    state_json LONGTEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_risk_checkpoints_key (checkpoint_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
