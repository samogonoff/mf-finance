-- 0067 — право cost:fot на ФОТ-дашборд («ФОТ и численность», P9–P13).
--
-- Паттерн 0045/0058: пока дашборд видят только те, кто и так видит все цифры
-- раздела, — ПЭО и Full Admin. Расширить список — отдельным решением.

UPDATE cost_roles
   SET permissions = permissions || '["cost:fot"]'::jsonb
 WHERE name IN ('ПЭО', 'Full Admin')
   AND NOT permissions @> '["cost:fot"]'::jsonb;
