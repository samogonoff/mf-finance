-- 0012 — add 3 price columns (Цена РФ, Цена КЗ, Цена УЗ) to cost_price_pending + cost_price_changes_audit
ALTER TABLE cost_price_pending
    ADD COLUMN IF NOT EXISTS "Цена РФ" NUMERIC,
    ADD COLUMN IF NOT EXISTS "Цена КЗ" NUMERIC,
    ADD COLUMN IF NOT EXISTS "Цена УЗ" NUMERIC;

ALTER TABLE cost_price_changes_audit
    ADD COLUMN IF NOT EXISTS price_rf NUMERIC(18,2),
    ADD COLUMN IF NOT EXISTS price_kz NUMERIC(18,2),
    ADD COLUMN IF NOT EXISTS price_uz NUMERIC(18,2);
