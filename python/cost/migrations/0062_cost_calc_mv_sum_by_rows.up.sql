-- 0062 — витрина cost_calc_mv: статьи и итоги себестоимости — сумма по строкам
-- калькуляции, а не max() (решение 02.10.2026).
--
-- ЗАЧЕМ
-- =====
-- В источнике каждая строка калькуляции несёт стоимость СВОЕГО материала или
-- операции, а max() по строкам (0036, 0042) брал самую дорогую строку. У
-- 117915 / М26.3.1437 вспомогательные материалы — 0,0615 (пакет) вместо 0,1282 по
-- шести строкам. Сырьё на штуку выпуска 2026 года выходило 2,18 BYN вместо 3,10,
-- у Orodoro — 0,09 вместо 0,30 (замер 01.10.2026 на копии CostHistory).
--
-- «Прочего» нет. Сумма шести статей по строкам равна «Себестоимость, руб.» у всех
-- калькуляций, кроме одной; то же в USD и для «Себестоимость факт» с фактическими
-- пошивом и раскроем. Остаток «прочее» (19% себестоимости в структуре
-- коммерческого дашборда, «прямые затраты покрывают 79%») был следствием max().
-- Пошив, раскрой и минуты лежат одной строкой — сумма для них совпадает с
-- прежним max(); там, где строк операции две, сумма и есть верный итог.
--
-- ИТОГ — ТОЖЕ СУММА СТАТЕЙ
-- =======================
-- «Себестоимость, руб.» на строке — итог ИСТОЧНИКА. Версии калькуляций и наборы
-- цен плана правят статьи строк, но не этот итог, поэтому дашборды показывали
-- себестоимость без правок, а главная таблица (сумма статей, /aggregated) — с
-- ними. Теперь cost_byn/cost_usd — сумма статей, как в главной таблице;
-- cost_fact_* — материалы, декоры и вязание плюс фактические пошив и раскрой.
--
-- Пустой итог источника остаётся пустым: у 25 тыс. «калькуляций» ФКСС — это
-- одиночные строки без модели, объёма и цены — итога нет, и сумма статей не
-- должна впускать их в выборки, где себестоимость — условие отбора. Факт до
-- 2026 года тоже остаётся NULL — его тогда не вели (0042).
--
-- ПЕРЕСОЗДАНИЕ
-- ============
-- Определение материализованного представления ALTER не меняет — пересоздаём.
-- Владелец, права (у витрины грант роли только для чтения df_ro) переносятся,
-- как в 0061: DROP их сносит. На время пересоздания витрины нет (секунды),
-- читатели получат ошибку, а не старые цифры — как в 0042.
CREATE TEMP TABLE _cost_calc_mv_acl AS
SELECT pg_get_userbyid(c.relowner) AS owner, c.relacl AS acl
  FROM pg_class c
 WHERE c.oid = to_regclass('public.cost_calc_mv');

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
    -- Реквизиты калькуляции одинаковы на всех её строках — max() выбирает
    -- единственное значение.
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
    -- Итоги — сумма статей по строкам (см. шапку); NULL, где итога нет в источнике.
    CASE WHEN max("Себестоимость, руб.") IS NOT NULL THEN
        coalesce(sum("Основные материалы, руб."), 0) + coalesce(sum("Вспомогательные материалы, руб."), 0)
      + coalesce(sum("Пошив, руб."), 0) + coalesce(sum("Раскрой, руб."), 0)
      + coalesce(sum("Декоры, руб."), 0) + coalesce(sum("Вязание, руб."), 0)
    END                                     AS cost_byn,
    CASE WHEN max("Себестоимость, USD.") IS NOT NULL THEN
        coalesce(sum("Основные материалы, USD."), 0) + coalesce(sum("Вспомогательные материалы, USD."), 0)
      + coalesce(sum("Пошив, USD."), 0) + coalesce(sum("Раскрой, USD."), 0)
      + coalesce(sum("Декоры, USD."), 0) + coalesce(sum("Вязание, USD."), 0)
    END                                     AS cost_usd,
    -- Статьи: у каждой строки своя сумма, статья калькуляции — их сумма.
    sum("Основные материалы, руб.")         AS mat_main_byn,
    sum("Основные материалы, USD.")         AS mat_main_usd,
    sum("Вспомогательные материалы, руб.")  AS mat_aux_byn,
    sum("Вспомогательные материалы, USD.")  AS mat_aux_usd,
    sum("Пошив, руб.")                      AS sewing_byn,
    sum("Пошив, USD.")                      AS sewing_usd,
    sum("Раскрой, руб.")                    AS cutting_byn,
    sum("Раскрой, USD.")                    AS cutting_usd,
    sum("Декоры, руб.")                     AS decor_byn,
    sum("Декоры, USD.")                     AS decor_usd,
    sum("Вязание, руб.")                    AS knitting_byn,
    sum("Вязание, USD.")                    AS knitting_usd,
    max("Курс на дату расчета")             AS fx_rate,
    max("Ставка НДС")                       AS vat_rate,
    sum("Пошив, минуты")                    AS sewing_minutes,
    sum("Раскрой, минуты")                  AS cutting_minutes,
    -- Факт по стоимости минуты (0042) — тем же правилом.
    sum("Пошив факт, минуты")               AS sewing_fact_minutes,
    sum("Пошив факт, руб.")                 AS sewing_fact_byn,
    sum("Пошив факт, USD.")                 AS sewing_fact_usd,
    sum("Раскрой факт, минуты")             AS cutting_fact_minutes,
    sum("Раскрой факт, руб.")               AS cutting_fact_byn,
    sum("Раскрой факт, USD.")               AS cutting_fact_usd,
    CASE WHEN max("Себестоимость факт, руб.") IS NOT NULL THEN
        coalesce(sum("Основные материалы, руб."), 0) + coalesce(sum("Вспомогательные материалы, руб."), 0)
      + coalesce(sum("Пошив факт, руб."), 0) + coalesce(sum("Раскрой факт, руб."), 0)
      + coalesce(sum("Декоры, руб."), 0) + coalesce(sum("Вязание, руб."), 0)
    END                                     AS cost_fact_byn,
    CASE WHEN max("Себестоимость факт, USD.") IS NOT NULL THEN
        coalesce(sum("Основные материалы, USD."), 0) + coalesce(sum("Вспомогательные материалы, USD."), 0)
      + coalesce(sum("Пошив факт, USD."), 0) + coalesce(sum("Раскрой факт, USD."), 0)
      + coalesce(sum("Декоры, USD."), 0) + coalesce(sum("Вязание, USD."), 0)
    END                                     AS cost_fact_usd,
    count(*)                                AS material_rows,
    (count(DISTINCT "Семья") > 1 OR count(DISTINCT "Пошив, руб.") > 1)
                                            AS is_ambiguous
FROM cost_data_cache
GROUP BY
    "Модель", "Артикул", "Признак калькуляции",
    "PLAN_ID", "дата расчета", "Номер задания производства"
WITH DATA;

-- Уникальный индекс обязателен: без него не работает REFRESH … CONCURRENTLY.
CREATE UNIQUE INDEX IF NOT EXISTS cost_calc_mv_key_idx
    ON cost_calc_mv (calc_key);
CREATE INDEX IF NOT EXISTS cost_calc_mv_sign_idx   ON cost_calc_mv (calc_sign);
CREATE INDEX IF NOT EXISTS cost_calc_mv_date_idx   ON cost_calc_mv (calc_date);
CREATE INDEX IF NOT EXISTS cost_calc_mv_model_idx  ON cost_calc_mv (model, articul);
CREATE INDEX IF NOT EXISTS cost_calc_mv_prod_idx   ON cost_calc_mv (production_date);

COMMENT ON MATERIALIZED VIEW cost_calc_mv IS
    'Калькуляции (свёртка cost_data_cache до натурального ключа). Обновляется '
    'после refresh кэша — см. set_cache_completed() в app/db.py. Статьи, минуты и '
    'итоги — сумма по строкам калькуляции (0062): cost_byn = сумма шести статей, '
    'поэтому учитывает версии и наборы цен плана, как главная таблица. NULL — '
    'итога нет в источнике. Суммы *_byn в белорусских рублях. *_fact_* — по '
    'фактической стоимости минуты (с 2026), до этого NULL.';

DO $migration$
DECLARE
    v_owner name;
    v_acl   aclitem[];
    r       record;
BEGIN
    SELECT owner, acl INTO v_owner, v_acl FROM _cost_calc_mv_acl;
    IF v_owner IS NOT NULL AND v_owner IS DISTINCT FROM current_user THEN
        EXECUTE format('ALTER MATERIALIZED VIEW public.cost_calc_mv OWNER TO %I', v_owner);
    END IF;
    -- Права владельца приходят с владением; остальные — как были.
    -- Без coalesce: у витрины без явных грантов relacl = NULL (так на проде),
    -- aclexplode(NULL) даёт ноль строк, а пустой '{}' — нульмерный массив, и
    -- функция падает «ACL arrays must be one-dimensional» (выкатка 02.10.2026).
    FOR r IN
        SELECT a.grantee, a.privilege_type, a.is_grantable
          FROM aclexplode(v_acl) a
         WHERE a.grantee IS DISTINCT FROM (SELECT oid FROM pg_roles WHERE rolname = v_owner)
    LOOP
        EXECUTE format('GRANT %s ON public.cost_calc_mv TO %s%s',
                       r.privilege_type,
                       CASE WHEN r.grantee = 0 THEN 'PUBLIC'
                            ELSE quote_ident(pg_get_userbyid(r.grantee)) END,
                       CASE WHEN r.is_grantable THEN ' WITH GRANT OPTION' ELSE '' END);
    END LOOP;
END
$migration$;

DROP TABLE _cost_calc_mv_acl;
