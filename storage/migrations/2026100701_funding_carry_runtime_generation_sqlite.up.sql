CREATE TABLE IF NOT EXISTS funding_carry_runtime_generations (
    scope_key TEXT NOT NULL PRIMARY KEY,
    generation INTEGER NOT NULL CHECK (generation >= 0),
    owner_token TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
