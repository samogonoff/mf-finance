-- Откат 0053: карточки согласования по постановлению 713 и их история
-- удаляются вместе с правом. Решения исполкома теряются — перед откатом на
-- проде выгрузить cost_reg713 и cost_reg713_log.

DROP TABLE IF EXISTS cost_reg713_log;
DROP TABLE IF EXISTS cost_reg713;

UPDATE cost_roles
   SET permissions = permissions - 'cost:reg713',
       updated_at = now()
 WHERE permissions @> '["cost:reg713"]'::jsonb;
