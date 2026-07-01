-- 0016 — версионное редактирование расчётов (черновики)

CREATE TABLE IF NOT EXISTS cost_calc_versions (
    id              BIGSERIAL PRIMARY KEY,

    model           TEXT NOT NULL,
    articul         TEXT NOT NULL,
    calc_sign       TEXT,
    plan_id         TEXT,
    "дата расчета"  DATE,

    version         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'pending', 'approved', 'rejected', 'archived')),
    source_refreshed_at TIMESTAMPTZ,

    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_by     TEXT,
    approved_at     TIMESTAMPTZ,
    comment         TEXT,

    UNIQUE (model, articul, calc_sign, plan_id, "дата расчета", version)
);

-- Строки версии (полная копия полей cost_data_cache + служебные)
CREATE TABLE IF NOT EXISTS cost_calc_version_rows (
    id              BIGSERIAL PRIMARY KEY,
    version_id      BIGINT NOT NULL REFERENCES cost_calc_versions(id) ON DELETE CASCADE,

    "Бренд-менеджер"                   TEXT,
    "Модель"                           TEXT NOT NULL,
    "Артикул"                          TEXT NOT NULL,
    "Признак калькуляции"              TEXT,
    "дата расчета"                     DATE,
    "дата производства"                DATE,
    "Курс на дату расчета"             NUMERIC(18,4),
    "Уровень цен"                      TEXT,
    "Страна пр-ва"                     TEXT,
    "Семья"                             TEXT,
    "Сезон"                             TEXT,
    "Level 01"                         TEXT,
    "Level 02"                         TEXT,
    "Level 03"                         TEXT,
    "Level 04"                         TEXT,
    "Level 05"                         TEXT,
    "Наименование модели"              TEXT,
    "Номер задания производства"       TEXT,
    "PLAN_ID"                          TEXT,
    "Материал/операция/декор(призн)"   TEXT,
    "Наименование"                     TEXT,
    "артикул материала"                TEXT,
    "свойство1"                        TEXT,
    "свойство2"                        TEXT,
    "свойство3"                        TEXT,
    "Норма"                            NUMERIC(18,4),
    "цена материала, руб."             NUMERIC(18,2),
    "цена материала, USD."             NUMERIC(18,2),
    "Ставка НДС"                       NUMERIC(18,2),
    "Розничная цена по уровню, руб."   NUMERIC(18,2),
    "Отпускная цена по уровню, руб"    NUMERIC(18,2),
    "Розничная цена по уровню, USD."   NUMERIC(18,2),
    "Отпускная цена по уровню, USD."   NUMERIC(18,2),
    "Основные материалы, руб."         NUMERIC(18,2),
    "Основные материалы, USD."         NUMERIC(18,2),
    "Вспомогательные материалы, руб."  NUMERIC(18,2),
    "Вспомогательные материалы, USD."  NUMERIC(18,2),
    "Пошив, руб."                      NUMERIC(18,2),
    "Пошив, USD."                      NUMERIC(18,2),
    "Раскрой, руб."                    NUMERIC(18,2),
    "Раскрой, USD."                    NUMERIC(18,2),
    "Декоры, руб."                     NUMERIC(18,2),
    "Декоры, USD."                     NUMERIC(18,2),
    "Вязание, руб."                    NUMERIC(18,2),
    "Вязание, USD."                    NUMERIC(18,2),
    "Себестоимость, руб."              NUMERIC(18,2),
    "Себестоимость, USD."              NUMERIC(18,2),

    -- Служебные поля строки
    sort_order       INTEGER NOT NULL DEFAULT 0,
    change_type      TEXT DEFAULT 'original'
                     CHECK (change_type IN ('original', 'added', 'modified', 'deleted')),
    row_comment      TEXT
);

CREATE INDEX IF NOT EXISTS idx_ver_model ON cost_calc_versions (model, articul);
CREATE INDEX IF NOT EXISTS idx_ver_status ON cost_calc_versions (status);
CREATE INDEX IF NOT EXISTS idx_ver_rows_version ON cost_calc_version_rows (version_id);
