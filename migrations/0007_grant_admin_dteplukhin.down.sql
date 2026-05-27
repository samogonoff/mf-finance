-- Откат 0007 — снимает ROLE_ADMIN с d.teplukhin@markformelle.by, остальные
-- прямые роли сохраняем. Если массив окажется пустым (роль была единственной) —
-- ставим явный пустой массив; ROLE_USER всё равно подмешивается ExpandRoles.

UPDATE users
SET roles = COALESCE(
        (SELECT jsonb_agg(v)
           FROM jsonb_array_elements_text(roles) AS t(v)
          WHERE v <> 'ROLE_ADMIN'),
        '[]'::jsonb
    ),
    updated_at = NOW()
WHERE lower(email) = lower('d.teplukhin@markformelle.by');
