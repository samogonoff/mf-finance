-- 0007 — откат: удаляем «Курс на дату расчета» из обеих таблиц

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "Курс на дату расчета";

ALTER TABLE cost_price_pending
    DROP COLUMN IF EXISTS "Курс на дату расчета";
