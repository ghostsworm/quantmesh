-- estimated: <1s on a new table; no existing order data is modified.
CREATE TABLE IF NOT EXISTS execution_intents (
    id BIGINT NOT NULL AUTO_INCREMENT,
    scope_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    client_order_id VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    revision BIGINT NOT NULL,
    state_json LONGTEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_execution_intents_scope_cid (scope_key, client_order_id),
    KEY idx_execution_intents_scope_id (scope_key, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
