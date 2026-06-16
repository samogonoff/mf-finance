-- 0011 down: remove refreshing_since column

ALTER TABLE cost_cache_status
    DROP COLUMN IF EXISTS refreshing_since;
