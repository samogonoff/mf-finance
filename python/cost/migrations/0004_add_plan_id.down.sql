-- 0004 — откат: удаляем PLAN_ID из cost_data_cache

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "PLAN_ID";
