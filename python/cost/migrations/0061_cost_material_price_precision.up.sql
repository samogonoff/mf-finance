-- 0061 — цена материала без ограничения знаков после запятой (задача Б24 661229,
-- 01.10.2026).
--
-- «цена материала, руб./USD.» хранилась как numeric(18,4) — по образцу money(19,4)
-- источника. Источнику этого хватает: цена там в валюте закупки (сумы, рос.
-- рубли), а округление поглощает коэффициент cost_factor_rub (миграция 0031).
-- Но в редакторе версий цену теперь можно перевести в другую валюту, и цена
-- единицы нормы в рублях у ниток — 0,000546: в четыре знака она ложилась как
-- 0,0005. Сервер считает статью от неокруглённой цены из запроса, а хранит
-- округлённую — после повторного открытия сумма строки уплывала на 8%, а
-- следующее «Сохранить» записывало её в себестоимость. То же было и при ручном
-- вводе рублёвой цены нитки больше чем с четырьмя знаками.
--
-- numeric без модификатора, а не numeric(18,8): снятие модификатора двоично
-- совместимо, таблицы не переписываются (в cost_data_cache около миллиона строк),
-- блокировка держится только на время правки каталога.
--
-- Колонки входят в представление cost_data_all, а тип колонки, которую
-- использует представление, Postgres менять не даёт. Поэтому представление
-- пересоздаётся тем же текстом (pg_get_viewdef), с тем же владельцем, правами и
-- комментарием: у него есть грант роли только для чтения df_ro и описание из
-- 0043, и без восстановления они бы пропали. Всё внутри одного DO — атомарно.
DO $migration$
DECLARE
    v_def     text;
    v_owner   name;
    v_acl     aclitem[];
    v_comment text;
    r         record;
BEGIN
    SELECT pg_get_viewdef(c.oid, true), pg_get_userbyid(c.relowner), c.relacl,
           obj_description(c.oid, 'pg_class')
      INTO v_def, v_owner, v_acl, v_comment
      FROM pg_class c
     WHERE c.oid = 'public.cost_data_all'::regclass;

    EXECUTE 'DROP VIEW public.cost_data_all';

    ALTER TABLE cost_data_cache
        ALTER COLUMN "цена материала, руб." TYPE numeric,
        ALTER COLUMN "цена материала, USD." TYPE numeric;
    ALTER TABLE cost_manual_calc
        ALTER COLUMN "цена материала, руб." TYPE numeric,
        ALTER COLUMN "цена материала, USD." TYPE numeric;
    ALTER TABLE cost_calc_version_rows
        ALTER COLUMN "цена материала, руб." TYPE numeric,
        ALTER COLUMN "цена материала, USD." TYPE numeric;

    EXECUTE 'CREATE VIEW public.cost_data_all AS ' || v_def;
    IF v_owner IS DISTINCT FROM current_user THEN
        EXECUTE format('ALTER VIEW public.cost_data_all OWNER TO %I', v_owner);
    END IF;
    IF v_comment IS NOT NULL THEN
        EXECUTE format('COMMENT ON VIEW public.cost_data_all IS %L', v_comment);
    END IF;
    -- Права владельца приходят с владением; остальные — как были.
    FOR r IN
        SELECT a.grantee, a.privilege_type, a.is_grantable
          FROM aclexplode(v_acl) a
         WHERE a.grantee IS DISTINCT FROM (SELECT oid FROM pg_roles WHERE rolname = v_owner)
    LOOP
        EXECUTE format('GRANT %s ON public.cost_data_all TO %s%s',
                       r.privilege_type,
                       CASE WHEN r.grantee = 0 THEN 'PUBLIC'
                            ELSE quote_ident(pg_get_userbyid(r.grantee)) END,
                       CASE WHEN r.is_grantable THEN ' WITH GRANT OPTION' ELSE '' END);
    END LOOP;
END
$migration$;
