-- 0022 — откат: удалить calc_sign и plan_id из cost_price_changes_audit

DROP INDEX IF EXISTS idx_audit_calc_key;

ALTER TABLE cost_price_changes_audit
    DROP COLUMN IF EXISTS calc_sign,
    DROP COLUMN IF EXISTS plan_id;