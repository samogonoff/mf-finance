-- 0011 — add refreshing_since column to cost_cache_status
-- The column was added to the code (set_cache_refreshing, get_cache_status, etc.)
-- but the migration was never created. Applying retroactively.

ALTER TABLE cost_cache_status
    ADD COLUMN IF NOT EXISTS refreshing_since TIMESTAMPTZ;
