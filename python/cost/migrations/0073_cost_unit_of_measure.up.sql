-- 0073 — единица измерения нормы строки калькуляции (задача Б24 672935,
-- 06.11.2026).
--
-- ЗАЧЕМ
-- =====
-- В источнике [Checks].[dbo].[CostHistory] есть колонка «ед. изм.»: у материалов
-- это кг/м/шт/рулон/боб, у пошива и раскроя — «мин», у декора — «шт». В приложение
-- она не переносилась, и в модальном редакторе расчёта колонка «Норма» стояла без
-- единицы: 0,088 кг и 0,088 шт выглядели одинаково. Заказчик (Стельмачёнок Е.А.,
-- чат «Тестирование ценообразования на WEB») просит показать единицу рядом с
-- нормой.
--
-- Колонка только читается и показывается в редакторе расчёта; формулы
-- себестоимости её не используют. В главную таблицу она не выводится — там строка
-- агрегируется по материалам, и единица у группы была бы не одна.
--
-- Тип text, а не словарь: значения приходят из источника как есть (в MSSQL это
-- char с добивкой пробелами — их снимает _convert_mssql_row при загрузке кэша).
--
-- Колонку НЕ добавлять в _FINGERPRINT_COLUMNS (app/db.py): отпечаток снимков
-- источника менять нельзя, иначе все старые снимки посчитаются «источник
-- изменился» и строки получат ложный признак 🔄 — ровно как у «Раскрой, ШТ»
-- (миграция 0063).
--
-- ПЕРЕСОЗДАНИЕ ПРЕДСТАВЛЕНИЯ
-- ==========================
-- Колонки входят в cost_data_all, а CREATE OR REPLACE VIEW не даёт вставлять
-- колонки в середину списка — пересоздаём через pg_get_viewdef с заменой якоря,
-- как в 0063. Якорь '."Норма",' в определении есть ровно дважды — по одному на
-- ветку UNION ALL (cost_data_cache и cost_manual_calc); проверено на живой БД
-- контура 06.11.2026. Владелец, права (грант роли только для чтения df_ro),
-- комментарий переносятся вручную: DROP их сносит.
--
-- Повторный прогон безопасен: ALTER — IF NOT EXISTS, а вставка в определение
-- выполняется только если колонки в нём ещё нет (иначе второй прогон удвоил бы
-- её в списке SELECT и представление не создалось бы).
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

    ALTER TABLE cost_data_cache        ADD COLUMN IF NOT EXISTS "ед. изм." text;
    ALTER TABLE cost_manual_calc       ADD COLUMN IF NOT EXISTS "ед. изм." text;
    ALTER TABLE cost_calc_version_rows ADD COLUMN IF NOT EXISTS "ед. изм." text;

    IF position('"ед. изм."' in v_def) = 0 THEN
        v_def := replace(v_def, '."Норма",', '."Норма", "ед. изм.",');
    END IF;
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
