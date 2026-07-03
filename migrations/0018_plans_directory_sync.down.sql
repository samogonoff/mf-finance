-- 0018 down
DROP TABLE IF EXISTS plans_position;
DROP TABLE IF EXISTS plans_dir_sync_log;
DELETE FROM plans_directory WHERE code IN ('dir_store_to', 'dir_lfl');
ALTER TABLE plans_directory DROP COLUMN IF EXISTS last_sync_removed;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS last_sync_changed;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS last_sync_added;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS next_retry_at;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS retry_count;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS stale_after_seconds;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS cache_ttl_seconds;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS valid_to;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS valid_from;
ALTER TABLE plans_directory DROP COLUMN IF EXISTS version;
