-- Destructive rollback: export the journal first. Never resume trading from an empty replacement.
DROP TABLE IF EXISTS execution_intents;
