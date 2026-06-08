-- 0007 — Добавляем «Курс на дату расчета» в cost_data_cache и cost_price_pending.
-- Колонка уже используется в CACHE_COLUMNS (db.py), AGG_AVG_FIELDS (routes.py)
-- и _PENDING_CACHE_COLS (db.py).
--
-- После применения миграции существующий кеш остаётся работоспособен;
-- курс заполнится при следующем плановом или ручном обновлении кеша.
-- В pending-таблице курс будет сохраняться при новом upsert'е.

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Курс на дату расчета" NUMERIC(18,4);

ALTER TABLE cost_price_pending
    ADD COLUMN IF NOT EXISTS "Курс на дату расчета" NUMERIC(18,4);
