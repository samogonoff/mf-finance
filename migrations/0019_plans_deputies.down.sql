-- 0019 down
ALTER TABLE users DROP COLUMN IF EXISTS absence_synced_at;
ALTER TABLE users DROP COLUMN IF EXISTS absence_until;
ALTER TABLE users DROP COLUMN IF EXISTS absence_status;
DROP TABLE IF EXISTS plans_deputy;
DROP TABLE IF EXISTS plans_position_stage;
