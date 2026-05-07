-- 0001 — пользователи Finance Cabinet.
-- Поля повторяют MP-эталон (см. FINANCE_PORTING_GUIDE 3), но без MP-специфики
-- уведомлений/телеграма — эти поля будут добавлены отдельной миграцией позже,
-- когда понадобятся уведомления.

CREATE TABLE IF NOT EXISTS users (
    id          BIGSERIAL PRIMARY KEY,
    email       TEXT        NOT NULL UNIQUE,
    password    TEXT        NOT NULL DEFAULT '',
    name        TEXT        NOT NULL DEFAULT '',
    last_name   TEXT        NOT NULL DEFAULT '',
    roles       JSONB       NOT NULL DEFAULT '["ROLE_USER"]'::jsonb,
    b24_id      BIGINT,
    b24_domain  TEXT,
    is_blocked  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email));

-- analytics-схема: только для чтения, доступ выдаём отдельной ролью.
-- Контейнер python-analytics будет коннектиться под analytics_ro.
CREATE SCHEMA IF NOT EXISTS analytics;
