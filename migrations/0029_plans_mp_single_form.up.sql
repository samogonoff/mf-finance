-- Объединение форм МП: вместо двух заданий «Маркетплейсы large» и «Маркетплейсы
-- small» — одно задание «Маркетплейсы» на все площадки. Разделение по ЦФО остаётся
-- внутри формы (колонки-площадки сгруппированы по сегменту), а сводный просмотр
-- фильтруется по площадке и ЮЛ. Основание: docs/reports/plans/ux-redesign.md §7b.
--
-- Фильтр '{"segment":"all"}' = все площадки dir_marketplace независимо от сегмента
-- (matchingCfo, go/internal/plans/task_store.go).
--
-- Идемпотентно: повторный прогон ничего не ломает.

-- 1) Шаблон large превращаем в общий (id сохраняем — на него ссылаются pl_task).
UPDATE pl_task_template
   SET title      = 'Маркетплейсы',
       cfo_filter = '{"segment":"all"}'
 WHERE form_code = 'TPL-MP'
   AND stage_code = '1.1'
   AND title = 'Маркетплейсы large';

-- 2) Задания, сгенерированные по шаблону small, переводим на общий шаблон,
--    чтобы не оставлять висячих ссылок при его удалении.
UPDATE pl_task t
   SET template_id = (SELECT id FROM pl_task_template
                       WHERE form_code='TPL-MP' AND stage_code='1.1' AND title='Маркетплейсы'
                       ORDER BY id LIMIT 1)
 WHERE t.template_id IN (SELECT id FROM pl_task_template
                          WHERE form_code='TPL-MP' AND stage_code='1.1' AND title='Маркетплейсы small');

-- 3) Убираем шаблон small.
DELETE FROM pl_task_template
 WHERE form_code = 'TPL-MP' AND stage_code = '1.1' AND title = 'Маркетплейсы small';
