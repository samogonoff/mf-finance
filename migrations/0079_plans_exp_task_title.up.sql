-- Единое название: в календаре и заданиях этапа 1.5 было «Затраты ЦП», а формы и карточки называются «Расходы ЦП».
UPDATE pl_task_template SET title = 'Расходы ЦП' WHERE form_code = 'TPL-CFO-EXP' AND title = 'Затраты ЦП';
UPDATE pl_task SET title = 'Расходы ЦП' WHERE form_code = 'TPL-CFO-EXP' AND title = 'Затраты ЦП';
