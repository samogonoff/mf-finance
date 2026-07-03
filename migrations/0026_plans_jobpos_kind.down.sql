-- 0026 down
DELETE FROM plans_job_position WHERE kind='founder';
ALTER TABLE plans_job_position DROP COLUMN IF EXISTS kind;
