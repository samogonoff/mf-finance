-- Откат 0021: удаляем назначение пользователя и Full Admin, восстанавливаем права системных ролей до версии 0019

DELETE FROM cost_user_roles
WHERE email = 'a.bushilo@markformelle.by'
  AND role_id = (SELECT id FROM cost_roles WHERE name = 'Full Admin');

DELETE FROM cost_roles WHERE name = 'Full Admin';

UPDATE cost_roles SET
    permissions  = '["cost:view", "cost:approve", "cost:export"]'::jsonb,
    updated_at   = now()
WHERE name = 'ПЭО' AND is_system = TRUE;

UPDATE cost_roles SET
    permissions  = '["cost:view", "cost:edit_price", "cost:export"]'::jsonb,
    updated_at   = now()
WHERE name = 'Бренд-менеджер' AND is_system = TRUE;

UPDATE cost_roles SET
    permissions  = '["cost:view", "cost:edit_price", "cost:edit_materials", "cost:export"]'::jsonb,
    updated_at   = now()
WHERE name = 'Калькулятор' AND is_system = TRUE;
