-- Откат 0062: витрина снова max() по строкам (определение 0042), итог — колонка
-- источника «Себестоимость, руб./USD.». Владелец и права переносятся, как в up.
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
CREATE INDEX IF NOT EXISTS cost_calc_mv_prod_idx   ON cost_calc_mv (production_date);

COMMENT ON MATERIALIZED VIEW cost_calc_mv IS
    'Калькуляции (свёртка cost_data_cache до натурального ключа). Обновляется '
    'после refresh кэша — см. set_cache_completed() в app/db.py. Суммы *_byn '
    'в белорусских рублях. *_fact_* — по фактической стоимости минуты (с 2026), '
    'до этого NULL.';

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
    FOR r IN
        SELECT a.grantee, a.privilege_type, a.is_grantable
          FROM aclexplode(v_acl) a  -- NULL → ноль строк; '{}' здесь падает (см. up)
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
