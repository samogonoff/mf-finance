-- 0023 — откат: убрать task_number из cost_calc_approvals

ALTER TABLE cost_calc_approvals DROP CONSTRAINT IF EXISTS cost_calc_approvals_unique;
ALTER TABLE cost_calc_approvals ADD CONSTRAINT cost_calc_approvals_model_articul_calc_sign_plan_id_key
    UNIQUE (model, articul, calc_sign, plan_id);
ALTER TABLE cost_calc_approvals DROP COLUMN IF EXISTS task_number;
