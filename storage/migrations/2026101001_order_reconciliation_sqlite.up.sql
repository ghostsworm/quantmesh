CREATE TABLE IF NOT EXISTS order_reconciliation_cases (
    case_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    owner_json TEXT NOT NULL,
    expected_revision TEXT NOT NULL,
    reason TEXT NOT NULL,
    evidence_refs_json TEXT NOT NULL,
    status TEXT NOT NULL,
    revision INTEGER NOT NULL,
    operation_request_json TEXT NULL DEFAULT NULL,
    receipt_json TEXT NULL DEFAULT NULL,
    release_evidence_hash TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_reconciliation_status_created ON order_reconciliation_cases(status, created_at);
CREATE TABLE IF NOT EXISTS order_reconciliation_audit (
    audit_id TEXT PRIMARY KEY,
    case_id TEXT NOT NULL,
    case_revision INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    outcome TEXT NOT NULL,
    detail_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_reconciliation_audit_case ON order_reconciliation_audit(case_id, created_at);
