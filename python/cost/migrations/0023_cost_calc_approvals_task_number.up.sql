-- 0023 — добавить task_number в cost_calc_approvals для уникальности по номеру задания

ALTER TABLE cost_calc_approvals ADD COLUMN IF NOT EXISTS task_number TEXT;

-- Пересоздать UNIQUE constraint с task_number
ALTER TABLE cost_calc_approvals DROP CONSTRAINT IF EXISTS cost_calc_approvals_model_articul_calc_sign_plan_id_key;
ALTER TABLE cost_calc_approvals DROP CONSTRAINT IF EXISTS cost_calc_approvals_unique;
ALTER TABLE cost_calc_approvals ADD CONSTRAINT cost_calc_approvals_unique
    UNIQUE (model, articul, calc_sign, plan_id, task_number);
