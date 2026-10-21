-- Согласование этапа 1.6: отдельный список согласующих для каждого канала свода.
-- В существующих таблицах legal_entity используется как универсальный ключ области:
-- для этапа 1.6 это название канала, для этапа 3 — ЮЛ, для этапа 4 — пустая строка.
ALTER TABLE pl_final_approver DROP CONSTRAINT IF EXISTS pl_final_approver_stage_code_check;
ALTER TABLE pl_final_approver
    ADD CONSTRAINT pl_final_approver_stage_code_check CHECK (stage_code IN ('1.6', '3', '4'));

ALTER TABLE pl_final_policy DROP CONSTRAINT IF EXISTS pl_final_policy_stage_code_check;
ALTER TABLE pl_final_policy
    ADD CONSTRAINT pl_final_policy_stage_code_check CHECK (stage_code IN ('1.6', '3', '4'));

ALTER TABLE pl_final_decision DROP CONSTRAINT IF EXISTS pl_final_decision_stage_code_check;
ALTER TABLE pl_final_decision
    ADD CONSTRAINT pl_final_decision_stage_code_check CHECK (stage_code IN ('1.6', '3', '4'));
