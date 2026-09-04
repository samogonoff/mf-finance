-- Откат 0045 — снапшот факта маркетплейсов.
DROP TABLE IF EXISTS mp_fact_snapshot_run;
DROP INDEX IF EXISTS idx_mp_fact_snapshot_period;
DROP TABLE IF EXISTS mp_fact_snapshot;
