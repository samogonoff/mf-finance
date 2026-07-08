-- 0020 — добавляем колонки минут в cost_calc_version_rows (аналог 0018 для cost_data_cache)
ALTER TABLE cost_calc_version_rows
    ADD COLUMN IF NOT EXISTS "Пошив, минуты"  NUMERIC(18,2),
    ADD COLUMN IF NOT EXISTS "Раскрой, минуты" NUMERIC(18,2);
