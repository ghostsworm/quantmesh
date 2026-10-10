CREATE TABLE IF NOT EXISTS order_reconciliation_cases (
    case_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    owner_json JSON NOT NULL,
    expected_revision VARCHAR(191) NOT NULL,
    reason TEXT NOT NULL,
    evidence_refs_json JSON NOT NULL,
    status VARCHAR(32) NOT NULL,
    revision BIGINT NOT NULL,
    operation_request_json JSON NULL,
    receipt_json JSON NULL,
    release_evidence_hash VARCHAR(128) NOT NULL DEFAULT '',
    created_by VARCHAR(191) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (case_id),
    UNIQUE KEY uk_order_reconciliation_idempotency (idempotency_key),
    INDEX idx_order_reconciliation_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS order_reconciliation_audit (
    audit_id VARCHAR(64) NOT NULL,
    case_id VARCHAR(64) NOT NULL,
    case_revision BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    actor VARCHAR(191) NOT NULL,
    outcome VARCHAR(32) NOT NULL,
    detail_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (audit_id),
    INDEX idx_order_reconciliation_audit_case (case_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
