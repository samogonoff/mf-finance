-- 0014 — add Комментарий to cost_price_pending and comment to cost_price_changes_audit
ALTER TABLE cost_price_pending
    ADD COLUMN IF NOT EXISTS "Комментарий" TEXT;

ALTER TABLE cost_price_changes_audit
    ADD COLUMN IF NOT EXISTS comment TEXT;
