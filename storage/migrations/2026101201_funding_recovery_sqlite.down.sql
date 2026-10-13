-- Destructive rollback only. Export recovery facts, receipts, outbox and holds before execution.
DROP TABLE IF EXISTS funding_recovery_adjudications;
DROP TABLE IF EXISTS funding_recovery_submission_attempts;
DROP TABLE IF EXISTS funding_recovery_permits;
DROP TABLE IF EXISTS funding_recovery_holds;
DROP TABLE IF EXISTS funding_recovery_outbox;
DROP TABLE IF EXISTS funding_recovery_operations;
DROP TABLE IF EXISTS funding_recovery_events;
DROP TABLE IF EXISTS funding_recovery_actions;
DROP TABLE IF EXISTS funding_recovery_children;
DROP TABLE IF EXISTS funding_recovery_episodes;
