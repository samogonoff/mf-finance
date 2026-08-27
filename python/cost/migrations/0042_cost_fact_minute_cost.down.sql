-- Откат 0042: витрина без колонок факта (определение 0036), колонки факта долой.
DROP MATERIALIZED VIEW IF EXISTS cost_calc_mv;

CREATE MATERIALIZED VIEW cost_calc_mv AS
SELECT
    "Модель"                            AS model,
    "Артикул"                           AS articul,
    "Признак калькуляции"               AS calc_sign,
    "PLAN_ID"                           AS plan_id,
    "дата расчета"                      AS calc_date,
    "Номер задания производства"        AS zadanie,
    md5(
        coalesce("Модель", '')                     || E'\x01' ||
        coalesce("Артикул", '')                    || E'\x01' ||
        coalesce("Признак калькуляции", '')        || E'\x01' ||
        coalesce("PLAN_ID", '')                    || E'\x01' ||
        coalesce("дата расчета"::text, '')         || E'\x01' ||
        coalesce("Номер задания производства", '')
    )                                   AS calc_key,
    max("Бренд-менеджер")               AS brand_manager,
    max("Наименование модели")          AS model_name,
    max("Уровень цен")                  AS price_level,
    max("Страна пр-ва")                 AS country,
    max("Сезон")                        AS season,
    max("Level 01")                     AS level01,
    max("Level 02")                     AS level02,
    max("Level 03")                     AS level03,
    max("Level 04")                     AS level04,
    max("Level 05")                     AS level05,
    max("color")                        AS color,
    max("дата производства")            AS production_date,
    max("Семья")                        AS family,
    max("выпуск шт")                    AS volume_pcs,
    max("Розничная цена по уровню, руб.")   AS retail_price_byn,
    max("Розничная цена по уровню, USD.")   AS retail_price_usd,
    max("Отпускная цена по уровню, руб")    AS wholesale_price_byn,
    max("Отпускная цена по уровню, USD.")   AS wholesale_price_usd,
    max("Себестоимость, руб.")              AS cost_byn,
    max("Себестоимость, USD.")              AS cost_usd,
    max("Основные материалы, руб.")         AS mat_main_byn,
    max("Основные материалы, USD.")         AS mat_main_usd,
    max("Вспомогательные материалы, руб.")  AS mat_aux_byn,
    max("Вспомогательные материалы, USD.")  AS mat_aux_usd,
    max("Пошив, руб.")                      AS sewing_byn,
    max("Пошив, USD.")                      AS sewing_usd,
    max("Раскрой, руб.")                    AS cutting_byn,
    max("Раскрой, USD.")                    AS cutting_usd,
    max("Декоры, руб.")                     AS decor_byn,
    max("Декоры, USD.")                     AS decor_usd,
    max("Вязание, руб.")                    AS knitting_byn,
    max("Вязание, USD.")                    AS knitting_usd,
    max("Курс на дату расчета")             AS fx_rate,
    max("Ставка НДС")                       AS vat_rate,
    max("Пошив, минуты")                    AS sewing_minutes,
    max("Раскрой, минуты")                  AS cutting_minutes,
    count(*)                                AS material_rows,
    (count(DISTINCT "Семья") > 1 OR count(DISTINCT "Пошив, руб.") > 1)
                                            AS is_ambiguous
FROM cost_data_cache
GROUP BY
    "Модель", "Артикул", "Признак калькуляции",
    "PLAN_ID", "дата расчета", "Номер задания производства"
WITH DATA;

CREATE UNIQUE INDEX IF NOT EXISTS cost_calc_mv_key_idx
    ON cost_calc_mv (calc_key);
CREATE INDEX IF NOT EXISTS cost_calc_mv_sign_idx   ON cost_calc_mv (calc_sign);
CREATE INDEX IF NOT EXISTS cost_calc_mv_date_idx   ON cost_calc_mv (calc_date);
CREATE INDEX IF NOT EXISTS cost_calc_mv_model_idx  ON cost_calc_mv (model, articul);

ALTER TABLE cost_calc_version_rows
    DROP COLUMN IF EXISTS "Пошив факт, минуты",
    DROP COLUMN IF EXISTS "Пошив факт, руб.",
    DROP COLUMN IF EXISTS "Пошив факт, USD.",
    DROP COLUMN IF EXISTS "Раскрой факт, минуты",
    DROP COLUMN IF EXISTS "Раскрой факт, руб.",
    DROP COLUMN IF EXISTS "Раскрой факт, USD.",
    DROP COLUMN IF EXISTS "Себестоимость факт, руб.",
    DROP COLUMN IF EXISTS "Себестоимость факт, USD.";

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "Пошив факт, минуты",
    DROP COLUMN IF EXISTS "Пошив факт, руб.",
    DROP COLUMN IF EXISTS "Пошив факт, USD.",
    DROP COLUMN IF EXISTS "Раскрой факт, минуты",
    DROP COLUMN IF EXISTS "Раскрой факт, руб.",
    DROP COLUMN IF EXISTS "Раскрой факт, USD.",
    DROP COLUMN IF EXISTS "Себестоимость факт, руб.",
    DROP COLUMN IF EXISTS "Себестоимость факт, USD.";
