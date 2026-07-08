-- 0021 — обновление/создание ролей cost-раздела для прода
-- Обновляет права системных ролей (после добавления cost:peo_mark, cost:admin, cost:edit_materials),
-- создаёт Full Admin и назначает a.bushilo@markformelle.by

-- Обновляем существующие системные роли (если миграция 0019 уже применилась)
INSERT INTO cost_roles (name, permissions, is_system) VALUES
    ('ПЭО',          '["cost:view", "cost:approve", "cost:export", "cost:edit_materials"]'::jsonb, TRUE),
    ('Бренд-менеджер', '["cost:view", "cost:edit_price", "cost:export"]'::jsonb,                   TRUE),
    ('Калькулятор',   '["cost:view", "cost:peo_mark", "cost:edit_materials", "cost:export"]'::jsonb, TRUE)
ON CONFLICT (name) DO UPDATE SET
    permissions = EXCLUDED.permissions,
    updated_at  = now();

-- Создаём роль Full Admin (если ещё нет)
INSERT INTO cost_roles (name, permissions, is_system)
SELECT 'Full Admin', '["cost:view", "cost:edit_price", "cost:approve", "cost:edit_materials", "cost:export", "cost:admin"]'::jsonb, FALSE
WHERE NOT EXISTS (SELECT 1 FROM cost_roles WHERE name = 'Full Admin');

-- Назначаем a.bushilo@markformelle.by на Full Admin
INSERT INTO cost_user_roles (email, role_id, granted_by)
SELECT 'a.bushilo@markformelle.by', id, 'migration'
FROM cost_roles
WHERE name = 'Full Admin'
ON CONFLICT (email, role_id) DO NOTHING;
