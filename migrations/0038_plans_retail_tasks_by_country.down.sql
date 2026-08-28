-- Откат 0038 — возврат к одному заданию на всю розницу.
DELETE FROM pl_task_template WHERE form_code = 'TPL-TO-RETAIL';
INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order)
VALUES ('1.1', 'TPL-TO-RETAIL', 'Товарооборот розницы',
        '{"group_cfo1": "Розница"}', 'fill', 'none', 10);
