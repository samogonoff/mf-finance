INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order)
SELECT '3', 'TPL-LE-APPROVE', 'Согласование бюджета ЮЛ', '{}', 'approve', 'legal_entity', 60
WHERE NOT EXISTS (SELECT 1 FROM pl_task_template WHERE form_code = 'TPL-LE-APPROVE');

DROP TABLE IF EXISTS pl_final_decision;
DROP TABLE IF EXISTS pl_final_policy;
DROP TABLE IF EXISTS pl_final_approver;
