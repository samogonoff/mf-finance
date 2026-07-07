CREATE TABLE IF NOT EXISTS cost_roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cost_user_roles (
    id          BIGSERIAL PRIMARY KEY,
    email       TEXT NOT NULL,
    role_id     BIGINT NOT NULL REFERENCES cost_roles(id) ON DELETE CASCADE,
    granted_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (email, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_email ON cost_user_roles (email);

INSERT INTO cost_roles (name, permissions, is_system) VALUES
    ('ПЭО', '["cost:view", "cost:approve", "cost:export"]'::jsonb, TRUE),
    ('Бренд-менеджер', '["cost:view", "cost:edit_price", "cost:export"]'::jsonb, TRUE),
    ('Калькулятор', '["cost:view", "cost:edit_price", "cost:edit_materials", "cost:export"]'::jsonb, TRUE);
