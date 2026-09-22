-- Откат 0049: снять уникальный индекс и снова объединить шаблоны МП.
DROP INDEX IF EXISTS uq_pl_task_slot;

UPDATE pl_task t
   SET template_id = (SELECT id FROM pl_task_template
                       WHERE form_code='TPL-MP' AND stage_code='1.1' AND title='Маркетплейсы large'
                       ORDER BY id LIMIT 1)
 WHERE t.template_id IN (SELECT id FROM pl_task_template
                          WHERE form_code='TPL-MP' AND stage_code='1.1' AND title='Маркетплейсы small');

DELETE FROM pl_task_template
 WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы small';

UPDATE pl_task_template
   SET title = 'Маркетплейсы', cfo_filter = '{"segment":"all"}'
 WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы large';
