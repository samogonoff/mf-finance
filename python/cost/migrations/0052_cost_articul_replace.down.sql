-- Откат 0052: отметки «нужна замена артикула», журнал писем и справочник
-- адресатов удаляются вместе с правом. История отметок теряется — перед
-- откатом на проде выгрузить cost_articul_replace, если она кому-то нужна.

DROP TABLE IF EXISTS cost_articul_replace_mail;
DROP TABLE IF EXISTS cost_articul_replace_recipients;
DROP TABLE IF EXISTS cost_articul_replace;

UPDATE cost_roles
   SET permissions = permissions - 'cost:articul_replace',
       updated_at = now()
 WHERE permissions @> '["cost:articul_replace"]'::jsonb;
