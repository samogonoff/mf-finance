-- 0012 — ABAC-срез модуля «Тактические планы» (docs/reports/plans/SPEC.md §5.1, §7.3).
-- Справочник «Пользователь ↔ роль ↔ объект»: функциональная роль процесса
-- (filler, le_approver…) + привязка к объекту (этап × ЦФО × страна × ЮЛ).
-- Глобальные auth-роли (ROLE_PLANS_*) — гейт доступа; здесь — конкретный срез.
-- Носители ROLE_PLANS_ADMIN/ROLE_ADMIN обходят ABAC (полный доступ).

CREATE TABLE IF NOT EXISTS plans_user_scope (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,                  -- filler|dept_head|le_approver|…
    stage_code   TEXT NOT NULL DEFAULT '',       -- ограничение по этапу ('' = все)
    country      TEXT NOT NULL DEFAULT '',
    legal_entity TEXT NOT NULL DEFAULT '',
    code_cfo     JSONB NOT NULL DEFAULT '[]',    -- список разрешённых ЦФО (площадок)
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, role, stage_code, country, legal_entity)
);
CREATE INDEX IF NOT EXISTS idx_plans_user_scope_user ON plans_user_scope(user_id);
