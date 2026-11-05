CREATE TABLE IF NOT EXISTS daily_equity_evidence_versions (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	exchange VARCHAR(64) NOT NULL,
	account_scope VARCHAR(191) NOT NULL,
	local_date CHAR(10) NOT NULL,
	revision BIGINT NOT NULL,
	scope_revision BIGINT NOT NULL DEFAULT 0,
	timezone VARCHAR(64) NOT NULL,
	method VARCHAR(64) NOT NULL,
	evidence_hash CHAR(64) NOT NULL,
	payload_json LONGTEXT NOT NULL,
	created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
	UNIQUE KEY uk_daily_equity_evidence_revision (exchange, account_scope, local_date, revision),
	UNIQUE KEY uk_daily_equity_evidence_hash (exchange, account_scope, local_date, evidence_hash),
	KEY idx_daily_equity_evidence_scope_date (account_scope, exchange, local_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS daily_equity_report_versions (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	exchange VARCHAR(64) NOT NULL,
	account_scope VARCHAR(191) NOT NULL,
	local_date CHAR(10) NOT NULL,
	revision BIGINT NOT NULL,
	evidence_revision BIGINT NOT NULL,
	input_digest CHAR(64) NOT NULL,
	method VARCHAR(64) NOT NULL,
	equity_reconciliation_complete TINYINT(1) NOT NULL DEFAULT 0,
	report_json LONGTEXT NOT NULL,
	created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
	UNIQUE KEY uk_daily_equity_report_revision (exchange, account_scope, local_date, revision),
	KEY idx_daily_equity_report_scope_date (account_scope, exchange, local_date),
	KEY idx_daily_equity_report_input (account_scope, exchange, local_date, input_digest, method)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS daily_equity_current (
	exchange VARCHAR(64) NOT NULL,
	account_scope VARCHAR(191) NOT NULL,
	local_date CHAR(10) NOT NULL,
	evidence_revision BIGINT NOT NULL DEFAULT 0,
	evidence_hash CHAR(64) NOT NULL DEFAULT '',
	report_revision BIGINT NOT NULL DEFAULT 0,
	report_input_digest CHAR(64) NOT NULL DEFAULT '',
	conflicted TINYINT(1) NOT NULL DEFAULT 0,
	created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
	PRIMARY KEY (exchange, account_scope, local_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS daily_equity_conflicts (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	exchange VARCHAR(64) NOT NULL,
	account_scope VARCHAR(191) NOT NULL,
	local_date CHAR(10) NOT NULL,
	source VARCHAR(64) NOT NULL,
	source_id VARCHAR(191) NOT NULL,
	wallet VARCHAR(32) NOT NULL,
	reason VARCHAR(64) NOT NULL,
	existing_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
	incoming_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
	created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
	UNIQUE KEY uk_daily_equity_conflict (exchange, account_scope, local_date, source, source_id, wallet, reason),
	KEY idx_daily_equity_conflicts_scope_date (account_scope, exchange, local_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
