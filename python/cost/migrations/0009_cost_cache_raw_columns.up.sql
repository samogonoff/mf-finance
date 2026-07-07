-- 0009 — Добавляем колонки для raw-rows (исходные строки из CostHistory).
-- Источник: MSSQL [Checks].[dbo].[CostHistory] — дополнительные поля
-- материала/операции, свойств, нормы и цен.
--
-- После применения миграции существующий кеш остаётся работоспособен;
-- новые колонки заполнятся при следующем плановом или ручном обновлении кеша.

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Материал/операция/декор(призн)" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Наименование" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "артикул материала" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "свойство1" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "свойство2" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "свойство3" TEXT;

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Норма" NUMERIC(18,4);

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "цена материала, руб." NUMERIC(18,2);

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "цена материала, USD." NUMERIC(18,2);
