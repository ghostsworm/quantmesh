-- Destructive rollback only. Never run automatically; legacy revision values are ambiguous.
ALTER TABLE order_reconciliation_cases DROP COLUMN operation_evidence_hash;
ALTER TABLE order_reconciliation_cases DROP COLUMN expected_strategy_revision;
ALTER TABLE order_reconciliation_cases DROP COLUMN expected_intent_revision;
