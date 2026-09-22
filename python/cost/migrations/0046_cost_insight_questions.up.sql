-- 0046 — журнал углублённых исследований («почему такая маржа?»)
--
-- Блок «Разбор ИИ» (0045) даёт обзор по графикам секции. Здесь пользователь
-- задаёт конкретный вопрос по срезу, и агент ведёт многошаговое исследование:
-- вызывает инструменты раздела (проекции margin.dashboard), читает результаты и
-- строит вывод. Код — app/insight_agent.py.
--
-- Зачем отдельная таблица, а не cost_insight_runs. У исследования другая
-- природа записи: есть вопрос пользователя, есть ТРАССА вызовов инструментов, и
-- есть структурированный вывод (ответ + наблюдения + качество данных + что
-- проверить дальше). Складывать это в таблицу обзоров значило бы держать
-- половину колонок пустыми в обеих половинах журнала.
--
-- ТРАССА — главная колонка этой таблицы, а не отладочный мусор. Вывод агента о
-- причине («маржа завышена, потому что в источнике недозаполнено сырьё»)
-- проверяем только по ней: какие инструменты с какими фильтрами вызывались.
-- Без трассы в финансах остаётся «ИИ сказал», а это на совещании звучит как
-- факт. Она же отвечает на вопрос «почему исследование стоило 12 секунд».
--
-- Ключ кэша request_hash включает вопрос, срез, модель И момент последнего
-- обновления кэша данных (cost_cache_status.refreshed_at): в отличие от обзора,
-- где в хеш входят сами серии, здесь агент ходит за данными сам, и без метки
-- обновления один и тот же вопрос отдавал бы устаревший вывод после того, как
-- витрина обновилась.

CREATE TABLE IF NOT EXISTS cost_insight_questions (
    id                BIGSERIAL   PRIMARY KEY,
    request_hash      TEXT        NOT NULL,
    email             TEXT        NOT NULL,
    question          TEXT        NOT NULL,
    -- Срез дашборда на момент вопроса: период, валюта, фильтры.
    context           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    model             TEXT        NOT NULL DEFAULT '',
    -- Вывод: {"answer": "...", "findings": [...], "data_quality": "...",
    --         "next_steps": [...], "confidence": "high|medium|low"}
    result            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Трасса шагов: [{"step": 1, "tool": "metrics", "args": {...}, "ok": true}]
    trace             JSONB       NOT NULL DEFAULT '[]'::jsonb,
    steps             INTEGER     NOT NULL DEFAULT 0,
    -- Сколько РЕАЛЬНЫХ запросов к витрине потребовалось. Меньше числа шагов:
    -- ядро отдаёт весь дашборд разом, и несколько инструментов обслуживаются
    -- одним запросом (см. _Cache в insight_agent.py). Рост этого числа
    -- относительно steps — признак, что кэш перестал работать.
    sql_calls         INTEGER     NOT NULL DEFAULT 0,
    degraded          BOOLEAN     NOT NULL DEFAULT FALSE,
    error             TEXT        NOT NULL DEFAULT '',
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    cost              TEXT        NOT NULL DEFAULT '',
    elapsed_ms        INTEGER,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Кэш: последнее успешное исследование по хешу. Частичный индекс — неудачные
-- попытки в кэш попадать не должны, иначе сбой провайдера залипнет навсегда.
CREATE INDEX IF NOT EXISTS cost_insight_questions_cache_idx
    ON cost_insight_questions (request_hash, created_at DESC)
    WHERE degraded = FALSE;

-- «О чём людей спрашивают» и «сколько это стоит» — по journal за период.
CREATE INDEX IF NOT EXISTS cost_insight_questions_created_idx
    ON cost_insight_questions (created_at DESC);

COMMENT ON TABLE cost_insight_questions IS
    'Журнал и кэш углублённых исследований по вопросу пользователя (app/insight_agent.py). Колонка trace — трасса вызовов инструментов, по ней проверяется вывод агента о причинах.';
