-- Откат 0060: ручная цена ПФКСС. Роль удаляется вместе с назначениями
-- (cost_user_roles — ON DELETE CASCADE), признак в заявках теряется.

ALTER TABLE cost_price_pending DROP COLUMN IF EXISTS price_manual;

DELETE FROM cost_roles WHERE name = 'Бренд-менеджер ТЕКС';

UPDATE cost_roles
   SET permissions = permissions - 'cost:manual_price',
       updated_at = now()
 WHERE permissions @> '["cost:manual_price"]'::jsonb;
