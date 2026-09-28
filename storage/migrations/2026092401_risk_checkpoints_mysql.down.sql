-- Back up risk_checkpoints before rollback; deletion loses risk high-water history.
DROP TABLE IF EXISTS risk_checkpoints;
