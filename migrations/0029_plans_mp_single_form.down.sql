-- Откат объединения форм МП: возвращаем раздельные шаблоны large/small.
-- Задания, сгенерированные по объединённому шаблону, остаются на шаблоне large —
-- их пересоздаёт «Генерировать задания».

UPDATE pl_task_template
   SET title      = 'Маркетплейсы large',
       cfo_filter = '{"segment":"large"}'
 WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы';

INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order)
SELECT '1.1', 'TPL-MP', 'Маркетплейсы small', '{"segment":"small"}', 'fill', 'position', 25
WHERE NOT EXISTS (
    SELECT 1 FROM pl_task_template
     WHERE form_code='TPL-MP' AND stage_code='1.1' AND title='Маркетплейсы small');
