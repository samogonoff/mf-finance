-- 0022 — добавить calc_sign и plan_id в cost_price_changes_audit
-- Необходимо для точечного удаления DWH-записей при отклонении цены / админ-очистке

ALTER TABLE cost_price_changes_audit
    ADD COLUMN IF NOT EXISTS calc_sign TEXT,
    ADD COLUMN IF NOT EXISTS plan_id TEXT;

-- Индексы для быстрого поиска при точечном удалении
CREATE INDEX IF NOT EXISTS idx_audit_calc_key
    ON cost_price_changes_audit (model, articul, calc_sign, plan_id);