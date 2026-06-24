-- 006 — добавить company_id в dim_contract (для уже созданной 005; idempotent).
-- Нужен для per-company счётчика договоров в админке. После применения —
-- ре-bootstrap dim_contract (старые строки получат company_id=DEFAULT '').
ALTER TABLE finance.dim_contract
    ADD COLUMN IF NOT EXISTS company_id LowCardinality(String) DEFAULT '' AFTER doc_id;
