-- Возврат схемы к миграции 0078: этап 1.6 не хранит отдельные решения.
ALTER TABLE pl_final_approver DROP CONSTRAINT IF EXISTS pl_final_approver_stage_code_check;
ALTER TABLE pl_final_approver
    ADD CONSTRAINT pl_final_approver_stage_code_check CHECK (stage_code IN ('3', '4'));

ALTER TABLE pl_final_policy DROP CONSTRAINT IF EXISTS pl_final_policy_stage_code_check;
ALTER TABLE pl_final_policy
    ADD CONSTRAINT pl_final_policy_stage_code_check CHECK (stage_code IN ('3', '4'));

ALTER TABLE pl_final_decision DROP CONSTRAINT IF EXISTS pl_final_decision_stage_code_check;
ALTER TABLE pl_final_decision
    ADD CONSTRAINT pl_final_decision_stage_code_check CHECK (stage_code IN ('3', '4'));
