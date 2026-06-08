-- 0005 — таблица предложений по изменению цен (механизм согласования).
-- Вместо прямой записи в MSSQL OLAP, изменения сначала попадают сюда.
-- После утверждения (apply) — запись в FinSandBox и удаление из этой таблицы.
-- При отклонении строки просто удаляются.
--
-- Хранит полный слепок строки из cost_data_cache + служебные поля.
-- Одна строка на (Модель, Артикул, PLAN_ID, Признак калькуляции) — upsert.

CREATE TABLE IF NOT EXISTS cost_price_pending (
    id              BIGSERIAL    PRIMARY KEY,

    -- Служебные поля
    username        TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,
    review_comment  TEXT,

    -- Все поля из cost_data_cache (полный слепок строки)
    "Бренд-менеджер"                    TEXT,
    "Модель"                            TEXT,
    "Артикул"                           TEXT,
    "Признак калькуляции"               TEXT,
    "дата расчета"                      DATE,
    "Уровень цен"                       TEXT,
    "Страна пр-ва"                      TEXT,
    "Семья"                             TEXT,
    "Сезон"                             TEXT,
    "Level 01"                          TEXT,
    "Level 02"                          TEXT,
    "Level 03"                          TEXT,
    "Level 04"                          TEXT,
    "Level 05"                          TEXT,
    "Наименование модели"               TEXT,
    "Номер задания производства"        TEXT,
    "PLAN_ID"                           TEXT,
    "Розничная цена по уровню, руб."     NUMERIC(18,2),
    "Отпускная цена по уровню, руб"      NUMERIC(18,2),
    "Розничная цена по уровню, USD."     NUMERIC(18,2),
    "Отпускная цена по уровню, USD."     NUMERIC(18,2),
    "Основные материалы, руб."           NUMERIC(18,2),
    "Основные материалы, USD."           NUMERIC(18,2),
    "Вспомогательные материалы, руб."    NUMERIC(18,2),
    "Вспомогательные материалы, USD."    NUMERIC(18,2),
    "Пошив, руб."                        NUMERIC(18,2),
    "Пошив, USD."                        NUMERIC(18,2),
    "Раскрой, руб."                      NUMERIC(18,2),
    "Раскрой, USD."                      NUMERIC(18,2),
    "Декоры, руб."                       NUMERIC(18,2),
    "Декоры, USD."                       NUMERIC(18,2),
    "Вязание, руб."                      NUMERIC(18,2),
    "Вязание, USD."                      NUMERIC(18,2),
    "Себестоимость, руб."                NUMERIC(18,2),
    "Себестоимость, USD."                NUMERIC(18,2),

    -- Уникальность: одна строка на (Модель, Артикул, PLAN_ID, Признак калькуляции)
    CONSTRAINT uq_pending_row UNIQUE ("Модель", "Артикул", "PLAN_ID", "Признак калькуляции")
);

CREATE INDEX IF NOT EXISTS idx_pending_created ON cost_price_pending (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pending_model   ON cost_price_pending ("Модель");
CREATE INDEX IF NOT EXISTS idx_pending_articul ON cost_price_pending ("Артикул");
