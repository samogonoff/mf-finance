-- 0022 — вид должности: filler (ввод по ЦФО) / approver (согласует весь этап) /
-- coordinator (свод) / observer. Нужен, чтобы на маршруте разделить «основного
-- согласующего этапа» и «задания на ввод» (ответственные по ЦФО). ТЗ §матрица ACL.
ALTER TABLE plans_position ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'filler';

UPDATE plans_position SET kind = 'filler'      WHERE code IN ('filler','prod_planner');
UPDATE plans_position SET kind = 'approver'    WHERE code IN ('dept_head','director','sales_final','prod_approver','le_approver','final_approver');
UPDATE plans_position SET kind = 'coordinator' WHERE code = 'coordinator';
UPDATE plans_position SET kind = 'observer'    WHERE code = 'auditor';
