-- 0041 — песочница приёмника публикации в Postgres.
--
-- Зачем: пока PLANS_PUBLISH_ENABLED=0 (нет подтверждённого маппинга BI) или нет
-- соединения с Budgeting, публикация отвечала «приёмник недоступен — доступен
-- только предпросмотр», и весь путь записи (DELETE+INSERT в транзакции, обратное
-- чтение суммы, контроль «Σ введено − Σ записано = 0», идемпотентность,
-- MarkPublished) на деле не выполнялся ни разу. Проверить функционал было нечем.
--
-- Таблица — заменитель приёмника в БД finance: ровно девять колонок
-- FormToLoa*/VFORMTOLOAD* + target_table (одна таблица играет все приёмники) и
-- служебные поля прогона. Включается PLANS_PUBLISH_SANDBOX=1 (dev/тест).
--
-- ВАЖНО: уникального индекса по логическому ключу здесь СПЕЦИАЛЬНО нет —
-- в приёмниках Budgeting его тоже нет (ответ 20 ТЗ 28.08.2026: индексы есть, но
-- не уникальные). Уникальность в песочнице скрыла бы ровно те дубли, от которых
-- нас защищает только DELETE+INSERT сервиса, — песочница обязана вести себя как
-- приёмник, а не лучше него.

CREATE TABLE IF NOT EXISTS publish_sandbox (
    id           BIGSERIAL PRIMARY KEY,
    target_table TEXT NOT NULL,                  -- какой приёмник изображаем
    -- Девять колонок приёмника. КодЦФО/КодPL — TEXT, как и уходит в MSSQL
    -- (mssqlPublisher пишет их через fmt.Sprint): песочница не должна быть
    -- строже по типам, иначе расхождение всплывёт только в проде.
    param        TEXT NOT NULL,                  -- [Параметр]
    country      TEXT NOT NULL DEFAULT '',       -- [Страна]
    code_cfo     TEXT NOT NULL DEFAULT '',       -- [КодЦФО]
    group_cfo1   TEXT NOT NULL DEFAULT '',       -- [ГруппыЦФО1]
    group_cfo2   TEXT NOT NULL DEFAULT '',       -- [ГруппыЦФО2]
    cfo_name     TEXT NOT NULL DEFAULT '',       -- [ЦФО]
    code_pl      TEXT NOT NULL DEFAULT '',       -- [КодPL]
    plan_date    DATE NOT NULL,                  -- [Дата]
    value        NUMERIC(20,4) NOT NULL DEFAULT 0, -- [Значение]
    written_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индекс по логическому ключу — как кластерный индекс приёмника: ускоряет
-- DELETE/SUM по (Дата, Параметр, …), уникальности не даёт.
CREATE INDEX IF NOT EXISTS publish_sandbox_key_idx
    ON publish_sandbox (target_table, plan_date, param, country, code_cfo, code_pl);
