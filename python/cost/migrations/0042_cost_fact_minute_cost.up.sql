-- Фактическая себестоимость минуты: колонки источника в кэше и витрине.
--
-- ЗАЧЕМ
-- =====
-- С 01.01.2026 в Лисе ежемесячно заводится ФАКТИЧЕСКАЯ стоимость минуты пошива и
-- раскроя (константы POSHIV_MIN_F / KROY_MIN_F / PRINT_MIN_F по фирмам, история в
-- Gpartner.dbo.s_hystory) — в отличие от НОРМАТИВНОЙ (POSHIV_MIN / KROY_MIN), по
-- которой расценивается новая продукция. Заказчик (документ «Фактическая и
-- нормативная маржа выпуска», 27.08.2026) просит вести дашборды маржи выпуска по
-- фактической себестоимости, а отклонение фактической маржи от нормативной
-- вынести отдельным листом.
--
-- Источник [Checks].[dbo].[CostHistory] уже считает факт сам — колонки
-- «Пошив факт, минуты/руб./USD.», «Раскрой факт, …», «Себестоимость факт, руб./USD.»
-- заполнены с января 2026 у 96% строк ФКСС и отличаются от норматива у 63%.
-- В локальный кэш они не попадали: _producer пересекает SELECT * с CACHE_COLUMNS,
-- а их в списке не было. Добавляем колонки здесь и имена в CACHE_COLUMNS —
-- наполнятся на следующем ПОЛНОМ обновлении кэша (частичное берёт только два
-- последних месяца, а факт есть с января).
--
-- ДО ЯНВАРЯ 2026 ФАКТА НЕТ — и это не пропуск, а отсутствие явления: до
-- 01.01.2026 фактическую минуту не вели. В витрине факт остаётся NULL, дашборды
-- обязаны показывать норматив с подписью, а не подменять одно другим молча.
--
-- ОБЕ ТАБЛИЦЫ СНИМКА
-- ==================
-- cost_calc_version_rows хранит построчный снимок калькуляции тем же списком
-- CACHE_COLUMNS, и переприменение версий гоняет данные INSERT … SELECT между
-- таблицами. Колонка только в кэше ломает обновление кэша целиком (так упала
-- миграция 0036 — исправлено 0037). Поэтому обе таблицы, одной миграцией.
ALTER TABLE cost_data_cache
    ADD COLUMN IF NOT EXISTS "Пошив факт, минуты"       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Пошив факт, руб."         NUMERIC,
    ADD COLUMN IF NOT EXISTS "Пошив факт, USD."         NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, минуты"     NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, руб."       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, USD."       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Себестоимость факт, руб." NUMERIC,
    ADD COLUMN IF NOT EXISTS "Себестоимость факт, USD." NUMERIC;

ALTER TABLE cost_calc_version_rows
    ADD COLUMN IF NOT EXISTS "Пошив факт, минуты"       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Пошив факт, руб."         NUMERIC,
    ADD COLUMN IF NOT EXISTS "Пошив факт, USD."         NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, минуты"     NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, руб."       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Раскрой факт, USD."       NUMERIC,
    ADD COLUMN IF NOT EXISTS "Себестоимость факт, руб." NUMERIC,
    ADD COLUMN IF NOT EXISTS "Себестоимость факт, USD." NUMERIC;

COMMENT ON COLUMN cost_data_cache."Себестоимость факт, руб." IS
    'Себестоимость по ФАКТИЧЕСКОЙ стоимости минуты пошива и раскроя (константы '
    '*_MIN_F Лисы, ведутся помесячно с 01.01.2026). До 2026 года NULL — факта не '
    'было. Норматив — «Себестоимость, руб.».';

-- ВИТРИНА
-- =======
-- У материализованного представления колонки не добавить ALTER'ом — пересоздаём.
-- Определение повторяет 0036 плюс восемь колонок факта; на время пересоздания
-- витрины нет (секунды), читатели получат ошибку «relation does not exist», а не
-- старые данные — приемлемо для миграции, недопустимо для refresh (там CONCURRENTLY).
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
    -- Факт по стоимости минуты (0042). Постоянство внутри ключа — как у
    -- нормативных аналогов: те же строки операций, другая ставка.
    max("Пошив факт, минуты")               AS sewing_fact_minutes,
    max("Пошив факт, руб.")                 AS sewing_fact_byn,
    max("Пошив факт, USD.")                 AS sewing_fact_usd,
    max("Раскрой факт, минуты")             AS cutting_fact_minutes,
    max("Раскрой факт, руб.")               AS cutting_fact_byn,
    max("Раскрой факт, USD.")               AS cutting_fact_usd,
    max("Себестоимость факт, руб.")         AS cost_fact_byn,
    max("Себестоимость факт, USD.")         AS cost_fact_usd,
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
-- Дашборды маржи фильтруют и группируют по дате производства — до 0042 индекса
-- по ней не было, и каждый запрос шёл полным сканом витрины.
CREATE INDEX IF NOT EXISTS cost_calc_mv_prod_idx   ON cost_calc_mv (production_date);

COMMENT ON MATERIALIZED VIEW cost_calc_mv IS
    'Калькуляции (свёртка cost_data_cache до натурального ключа). Обновляется '
    'после refresh кэша — см. set_cache_completed() в app/db.py. Суммы *_byn '
    'в белорусских рублях. *_fact_* — по фактической стоимости минуты (с 2026), '
    'до этого NULL.';
