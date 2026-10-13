ALTER TABLE funding_recovery_source_cursors
    ADD COLUMN symbol VARCHAR(64) NOT NULL DEFAULT '' AFTER account_scope;
