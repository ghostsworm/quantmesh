CREATE TABLE IF NOT EXISTS funding_carry_runtime_generations (
    scope_key CHAR(64) NOT NULL,
    generation BIGINT NOT NULL,
    owner_token CHAR(64) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (scope_key),
    CONSTRAINT chk_funding_carry_runtime_generation CHECK (generation >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
