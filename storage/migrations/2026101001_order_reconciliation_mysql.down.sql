-- Destructive rollback only. Never run automatically; export cases and audit first.
DROP TABLE IF EXISTS order_reconciliation_audit;
DROP TABLE IF EXISTS order_reconciliation_cases;
