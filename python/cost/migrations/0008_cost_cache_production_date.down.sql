-- 0008 — откат: удаляем «дата производства» из cost_data_cache

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "дата производства";
