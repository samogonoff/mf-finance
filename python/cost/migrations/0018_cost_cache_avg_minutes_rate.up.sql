-- 0018 — добавляем колонки для новых AVG-полей (Пошив/Раскрой минуты, Курс)
ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Пошив, минуты"              NUMERIC(18,2),
    ADD COLUMN IF NOT EXISTS "Раскрой, минуты"             NUMERIC(18,2),
    ADD COLUMN IF NOT EXISTS "Курс на дату расчета"        NUMERIC(18,2);
