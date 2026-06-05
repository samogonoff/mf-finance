-- 0003 — таргеты маржинальности по level1.
-- Хранит целевой процент маржинальности для каждой группы level01.
-- История изменений: updated_at + updated_by фиксируют кто и когда менял.

CREATE TABLE IF NOT EXISTS cost_margin_targets (
    id                SERIAL       PRIMARY KEY,
    level1            TEXT         NOT NULL UNIQUE,
    target_margin_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by        TEXT         NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_by        TEXT         NOT NULL DEFAULT ''
);
