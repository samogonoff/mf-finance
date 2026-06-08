-- 0004 — Добавляем PLAN_ID в cost_data_cache.
-- Колонка уже используется в CACHE_COLUMNS (db.py) и AGG_GROUP_FIELDS (routes.py),
-- но была пропущена в 0002_cost_cache.up.sql, что вызывало UndefinedColumnError
-- при SELECT / GROUP BY в GET /api/cost/aggregated.
--
-- После применения миграции существующий кеш остаётся работоспособен;
-- PLAN_ID заполнится при следующем плановом или ручном обновлении кеша.

ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "PLAN_ID" TEXT;
