-- 0007 — выдаём ROLE_ADMIN пользователю d.teplukhin@markformelle.by.
-- Матчим по lower(email), а не по id: id присваивается BIGSERIAL и может
-- отличаться между контурами (dev/stage/prod). Идемпотентно: если ROLE_ADMIN
-- уже есть в массиве — JSONB не меняется. Остальные прямые роли сохраняем.
-- ROLE_USER выдаётся автоматически на выдаче токена через auth.ExpandRoles.

UPDATE users
SET roles = CASE
        WHEN roles @> '["ROLE_ADMIN"]'::jsonb THEN roles
        ELSE roles || '["ROLE_ADMIN"]'::jsonb
    END,
    updated_at = NOW()
WHERE lower(email) = lower('d.teplukhin@markformelle.by');
