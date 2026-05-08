-- 0001 — раздел «Себестоимость».
-- Локальная БД раздела (postgres-cost) НЕ хранит учётки — пользователи берутся
-- из основного контура (Go-API/finance). Здесь — только аудит изменений
-- уровней цен (дублирующая запись к OLAP CostHistory_Changes).

CREATE TABLE IF NOT EXISTS cost_price_changes_audit (
    id             BIGSERIAL    PRIMARY KEY,
    model          TEXT,
    articul        TEXT,
    price_level    TEXT,
    retail_rub     NUMERIC(18,2),
    wholesale_rub  NUMERIC(18,2),
    username       TEXT,
    changed_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cost_audit_changed_at ON cost_price_changes_audit (changed_at DESC);
