-- Откат 0056: статус «Неактуальная модель/артикул» и право на него.
-- Отметки теряются — перед откатом на проде выгрузить cost_obsolete, если нужны.

DROP TABLE IF EXISTS cost_obsolete;

UPDATE cost_roles
   SET permissions = permissions - 'cost:obsolete',
       updated_at = now()
 WHERE permissions @> '["cost:obsolete"]'::jsonb;
