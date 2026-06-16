-- 0010 — откат: удаляем «Ставка НДС» из cost_data_cache

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "Ставка НДС";
