-- 0033 — наборы цен на материалы для плана целиком («документы»).
--
-- Задача: править цены материалов сразу по всему PLAN_ID, не привязываясь к
-- модели, артикулу и заданию. Только для калькуляций с признаком КПСС.
--
-- Ключ материала — пять полей источника: Наименование, артикул материала,
-- свойство1..3. Проверено на живых данных: под КПСС такой ключ даёт 2045 групп
-- на 74 плана (~28 материалов на план), и лишь у 27 групп (1.3%) внутри плана
-- встречается больше одной цены — максимум 5. Для сравнения, ключ только по
-- «артикул материала» давал 37.4% групп с разными ценами и до 303 цен на один
-- материал. То есть пятипольный ключ практически однозначен, и средняя цена как
-- представитель группы оправдана. При применении набора вся группа получает ОДНУ
-- цену — разброс в тех 1.3% намеренно затирается.
--
-- Все поля ключа NOT NULL DEFAULT '': в PostgreSQL NULL'ы в UNIQUE считаются
-- различными, и уникальность строки набора с NULL просто не работала бы.
--
-- Приоритет применения к расчёту себестоимости (порядок наложения на кэш после
-- реимпорта из источника, см. load_cost_data_to_cache):
--   1) применённая версия калькуляции — накладывается последней и побеждает;
--   2) применённый набор цен плана — накладывается перед версиями;
--   3) исходные данные источника — если нет ни того, ни другого.
--
-- Курс — один на весь набор (решение пользователя), а не на строку, как в
-- редакторе версий: набор это «документ» на план, и разные курсы внутри одного
-- документа смысла не имеют.

CREATE TABLE IF NOT EXISTS cost_plan_price_sets (
    id          BIGSERIAL PRIMARY KEY,
    plan_id     TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'applied', 'archived')),
    rate        NUMERIC(18,4),
    comment     TEXT,
    created_by  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_by  TEXT,
    applied_at  TIMESTAMPTZ
);

-- «Применён ровно один набор на план, либо ни один». Отсутствие строки со
-- status='applied' — это и есть состояние «все наборы исключены из расчёта».
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_price_one_applied
    ON cost_plan_price_sets (plan_id) WHERE status = 'applied';

CREATE INDEX IF NOT EXISTS idx_plan_price_sets_plan
    ON cost_plan_price_sets (plan_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cost_plan_price_set_rows (
    id                  BIGSERIAL PRIMARY KEY,
    set_id              BIGINT NOT NULL REFERENCES cost_plan_price_sets(id) ON DELETE CASCADE,
    "Наименование"      TEXT NOT NULL DEFAULT '',
    "артикул материала" TEXT NOT NULL DEFAULT '',
    "свойство1"         TEXT NOT NULL DEFAULT '',
    "свойство2"         TEXT NOT NULL DEFAULT '',
    "свойство3"         TEXT NOT NULL DEFAULT '',
    price_rub           NUMERIC(18,4),
    price_usd           NUMERIC(18,4),
    -- Средняя цена группы на момент формирования набора — для показа «было → стало»
    -- и для возврата к исходной цене при снятии набора.
    source_price_rub    NUMERIC(18,4),
    source_price_usd    NUMERIC(18,4),
    -- Сколько строк кэша покрывала группа при формировании (информационно, для UI).
    rows_count          INTEGER NOT NULL DEFAULT 0,
    UNIQUE (set_id, "Наименование", "артикул материала", "свойство1", "свойство2", "свойство3")
);

CREATE INDEX IF NOT EXISTS idx_plan_price_set_rows_set
    ON cost_plan_price_set_rows (set_id);
