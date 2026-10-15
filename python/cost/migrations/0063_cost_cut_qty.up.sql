-- 0063 — «Раскрой, ШТ» из CostHistory (задача Б24 663722, 13.10.2026).
--
-- В источнике [Checks].[dbo].[CostHistory] с октября 2026 появилась колонка
-- «Раскрой, ШТ» — сколько единиц фактически выкроено по карте кроя задания.
-- Она позволяет отделить фактический раскрой от «выпуска шт» (который источник
-- размазывает по строкам материалов от 0 до полного тиража). Пока колонка
-- только добавляется в кэш/ручные строки/версии и показывается в главной
-- таблице; формулы себестоимости по ней не считаются.
--
-- Колонку НЕ добавлять в _FINGERPRINT_COLUMNS (app/db.py): отпечаток снимков
-- источника менять нельзя, иначе все старые снимки посчитаются «источник
-- изменился» и строки получат ложный признак 🔄.
--
-- Представление cost_data_all пересоздаётся через pg_get_viewdef с той же
-- заменой в обеих ветках UNION ALL, с сохранением владельца, прав и
-- комментария (паттерн миграции 0061). Вставка идёт сразу после «выпуск шт»,
-- а не в конец: CREATE OR REPLACE VIEW не даёт вставлять колонки в середину.
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

    ALTER TABLE cost_data_cache       ADD COLUMN IF NOT EXISTS "Раскрой, ШТ" numeric;
    ALTER TABLE cost_manual_calc      ADD COLUMN IF NOT EXISTS "Раскрой, ШТ" numeric;
    ALTER TABLE cost_calc_version_rows ADD COLUMN IF NOT EXISTS "Раскрой, ШТ" numeric;

    v_def := replace(v_def, '."выпуск шт",', '."выпуск шт", "Раскрой, ШТ",');
    EXECUTE 'CREATE VIEW public.cost_data_all AS ' || v_def;
    IF v_owner IS DISTINCT FROM current_user THEN
        EXECUTE format('ALTER VIEW public.cost_data_all OWNER TO %I', v_owner);
    END IF;
    IF v_comment IS NOT NULL THEN
        EXECUTE format('COMMENT ON VIEW public.cost_data_all IS %L', v_comment);
    END IF;
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
