-- 007 — отсрочка по договору в dim_contract (для уже созданной 005; idempotent).
-- Payments.Docs.Delay → срок/просрочка в отчёте и drilldown (ТЗ). После применения —
-- ре-bootstrap dim_contract (source=contract), чтобы заполнить.
ALTER TABLE finance.dim_contract
    ADD COLUMN IF NOT EXISTS payment_delay String DEFAULT '' AFTER account_kind;
