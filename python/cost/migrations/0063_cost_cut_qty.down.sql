-- 0063 down — убрать «Раскрой, ШТ» и вернуть представление без неё.
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

    -- pg_get_viewdef выводит каждую колонку отдельной строкой с префиксом
    -- таблицы — вырезаем обе (cost_data_cache и cost_manual_calc).
    v_def := regexp_replace(v_def, '\n\s+\w+\."Раскрой, ШТ",', '', 'g');
    EXECUTE 'CREATE VIEW public.cost_data_all AS ' || v_def;

    ALTER TABLE cost_data_cache        DROP COLUMN IF EXISTS "Раскрой, ШТ";
    ALTER TABLE cost_manual_calc       DROP COLUMN IF EXISTS "Раскрой, ШТ";
    ALTER TABLE cost_calc_version_rows DROP COLUMN IF EXISTS "Раскрой, ШТ";

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
