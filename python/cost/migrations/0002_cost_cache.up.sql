-- 0002 — кеш данных себестоимости для ускорения работы.
-- Источник: [Checks].[dbo].[CostHistory] (MSSQL) — тяжёлая
-- таблица, поэтому данные кешируются в локальный postgres-cost.
-- Кеш обновляется раз в 3 часа (планово) и по кнопке на фронте (принудительно).

CREATE TABLE IF NOT EXISTS cost_data_cache (
    id              BIGSERIAL    PRIMARY KEY,
    "Бренд-менеджер"                     TEXT,
    "Модель"                             TEXT,
    "Артикул"                            TEXT,
    "Признак калькуляции"                TEXT,
    "дата расчета"                       TIMESTAMPTZ,
    "Уровень цен"                        TEXT,
    "Страна пр-ва"                       TEXT,
    "Семья"                              TEXT,
    "Сезон"                              TEXT,
    "Level 01"                           TEXT,
    "Level 02"                           TEXT,
    "Level 03"                           TEXT,
    "Level 04"                           TEXT,
    "Level 05"                           TEXT,
    "Наименование модели"                TEXT,
    "Номер задания производства"         TEXT,
    "Розничная цена по уровню, руб."      NUMERIC(18,2),
    "Отпускная цена по уровню, руб"       NUMERIC(18,2),
    "Розничная цена по уровню, USD."      NUMERIC(18,2),
    "Отпускная цена по уровню, USD."      NUMERIC(18,2),
    "Основные материалы, руб."            NUMERIC(18,2),
    "Основные материалы, USD."            NUMERIC(18,2),
    "Вспомогательные материалы, руб."     NUMERIC(18,2),
    "Вспомогательные материалы, USD."     NUMERIC(18,2),
    "Пошив, руб."                         NUMERIC(18,2),
    "Пошив, USD."                         NUMERIC(18,2),
    "Раскрой, руб."                       NUMERIC(18,2),
    "Раскрой, USD."                       NUMERIC(18,2),
    "Декоры, руб."                        NUMERIC(18,2),
    "Декоры, USD."                        NUMERIC(18,2),
    "Вязание, руб."                       NUMERIC(18,2),
    "Вязание, USD."                       NUMERIC(18,2),
    "Себестоимость, руб."                 NUMERIC(18,2),
    "Себестоимость, USD."                 NUMERIC(18,2)
);

-- Индексы для ускорения фильтрации и группировки
CREATE INDEX IF NOT EXISTS idx_cache_brand_manager  ON cost_data_cache ("Бренд-менеджер");
CREATE INDEX IF NOT EXISTS idx_cache_model          ON cost_data_cache ("Модель");
CREATE INDEX IF NOT EXISTS idx_cache_articul        ON cost_data_cache ("Артикул");
CREATE INDEX IF NOT EXISTS idx_cache_date           ON cost_data_cache ("дата расчета");
CREATE INDEX IF NOT EXISTS idx_cache_calc_sign      ON cost_data_cache ("Признак калькуляции");

-- Статус кеша (singleton-строка)
CREATE TABLE IF NOT EXISTS cost_cache_status (
    id              INTEGER      PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    refreshed_at    TIMESTAMPTZ,
    row_count       INTEGER      NOT NULL DEFAULT 0,
    is_refreshing   BOOLEAN      NOT NULL DEFAULT FALSE,
    error_message   TEXT
);

-- Гарантируем, что строка существует
INSERT INTO cost_cache_status (id, refreshed_at, row_count, is_refreshing)
VALUES (1, NULL, 0, FALSE)
ON CONFLICT (id) DO NOTHING;
