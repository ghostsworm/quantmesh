CREATE TABLE IF NOT EXISTS daily_equity_evidence_versions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	exchange TEXT NOT NULL,
	account_scope TEXT NOT NULL,
	local_date TEXT NOT NULL,
	revision INTEGER NOT NULL,
	scope_revision INTEGER NOT NULL DEFAULT 0,
	timezone TEXT NOT NULL,
	method TEXT NOT NULL,
	evidence_hash TEXT NOT NULL,
	payload_json TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(exchange, account_scope, local_date, revision),
	UNIQUE(exchange, account_scope, local_date, evidence_hash)
);

CREATE INDEX IF NOT EXISTS idx_daily_equity_evidence_scope_date
	ON daily_equity_evidence_versions(account_scope, exchange, local_date);

CREATE TABLE IF NOT EXISTS daily_equity_report_versions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	exchange TEXT NOT NULL,
	account_scope TEXT NOT NULL,
	local_date TEXT NOT NULL,
	revision INTEGER NOT NULL,
	evidence_revision INTEGER NOT NULL,
	input_digest TEXT NOT NULL,
	method TEXT NOT NULL,
	equity_reconciliation_complete INTEGER NOT NULL DEFAULT 0,
	report_json TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(exchange, account_scope, local_date, revision)
);

CREATE INDEX IF NOT EXISTS idx_daily_equity_report_scope_date
	ON daily_equity_report_versions(account_scope, exchange, local_date);
CREATE INDEX IF NOT EXISTS idx_daily_equity_report_input
	ON daily_equity_report_versions(account_scope, exchange, local_date, input_digest, method);

CREATE TABLE IF NOT EXISTS daily_equity_current (
	exchange TEXT NOT NULL,
	account_scope TEXT NOT NULL,
	local_date TEXT NOT NULL,
	evidence_revision INTEGER NOT NULL DEFAULT 0,
	evidence_hash TEXT NOT NULL DEFAULT '',
	report_revision INTEGER NOT NULL DEFAULT 0,
	report_input_digest TEXT NOT NULL DEFAULT '',
	conflicted INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(exchange, account_scope, local_date)
);

CREATE TABLE IF NOT EXISTS daily_equity_conflicts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	exchange TEXT NOT NULL,
	account_scope TEXT NOT NULL,
	local_date TEXT NOT NULL,
	source TEXT NOT NULL,
	source_id TEXT NOT NULL,
	wallet TEXT NOT NULL,
	reason TEXT NOT NULL,
	existing_fingerprint TEXT NOT NULL DEFAULT '',
	incoming_fingerprint TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(exchange, account_scope, local_date, source, source_id, wallet, reason)
);

CREATE INDEX IF NOT EXISTS idx_daily_equity_conflicts_scope_date
	ON daily_equity_conflicts(account_scope, exchange, local_date);
