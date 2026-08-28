-- Откат 0043

DROP VIEW IF EXISTS cost_data_all;
DROP TABLE IF EXISTS cost_manual_calc;

UPDATE cost_roles
   SET permissions = permissions - 'cost:calc_sign_copy'
 WHERE permissions @> '["cost:calc_sign_copy"]'::jsonb;
