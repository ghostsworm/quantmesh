-- Add separate durable revision identities. Existing rows stay empty and fail closed;
-- legacy expected_revision is intentionally not copied into either new field.
ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision VARCHAR(191) NOT NULL DEFAULT '';
ALTER TABLE order_reconciliation_cases ADD COLUMN expected_strategy_revision VARCHAR(191) NOT NULL DEFAULT '';
ALTER TABLE order_reconciliation_cases ADD COLUMN operation_evidence_hash VARCHAR(128) NOT NULL DEFAULT '';
