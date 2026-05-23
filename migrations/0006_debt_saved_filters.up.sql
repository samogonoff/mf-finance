-- 0002 — сохранённые фильтры для отчёта «Задолженность ВГО».
-- payload хранит JSON {date_from, date_to, entity_inns[], accounts[], currencies[]}.
-- Привязано к users(id) (BIGSERIAL), не к UUID.

CREATE TABLE IF NOT EXISTS debt_saved_filters (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    payload     JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_debt_saved_filters_user ON debt_saved_filters(user_id);
