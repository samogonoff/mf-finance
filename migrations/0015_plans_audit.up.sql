-- 0015 — журнал аудита «Тактических планов» (AUD-01..05, SPEC §17).
-- Пишется при PLANS_AUDIT_ENABLED=1. События: создание PL, сохранение формы,
-- корректировка, override формулы, комментарий, назначение среза, импорт и т.д.

CREATE TABLE IF NOT EXISTS plans_audit_event (
    id             BIGSERIAL PRIMARY KEY,
    ts             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_id        BIGINT,
    action         TEXT NOT NULL,          -- save_form|adjustment|formula_override|comment|create_instance|scope|import
    entity_type    TEXT NOT NULL DEFAULT '',
    entity_id      BIGINT,
    before         JSONB,
    after          JSONB,
    ip             TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_plans_audit_ts ON plans_audit_event(ts DESC);
CREATE INDEX IF NOT EXISTS idx_plans_audit_user ON plans_audit_event(user_id);
CREATE INDEX IF NOT EXISTS idx_plans_audit_entity ON plans_audit_event(entity_type, entity_id);
