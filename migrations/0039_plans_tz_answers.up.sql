-- 0039 — ответы финблока по открытым вопросам ТЗ «Розница» §12 (27.08.2026).
-- Решения, которые лежат В ДАННЫХ, а не в коде:
--   п.4 — шаг «Финансист» подтверждён («подтверждаем, просто переход делаем»):
--         включаем его и ставим МЕЖДУ 1.2 и 1.3, как в §2.1 обоих ТЗ (в сиде
--         0031 он стоял до 1.2 — это расхождение с ТЗ, здесь оно исправлено).
--   п.7 — «да, публиковать»: правила публикации плана продаж розницы включены.
--   п.2 — BYN-пару приёмника заполняет ETL на стороне BI, приложение пишет
--         только национальную валюту (VFORMTOLOADTAKTTARGET), поэтому правил с
--         target_currency='BYN' не появляется. Запись всё равно остаётся под
--         PLANS_PUBLISH_ENABLED и белым списком PLANS_PUBLISH_TARGETS.

UPDATE plans_form_route
   SET enabled = TRUE,
       sort_order = 35,
       updated_at = NOW()
 WHERE step_code = 'fin'
   AND form_code IN ('TPL-MP', 'TPL-TO-RETAIL');

UPDATE publish_mapping
   SET enabled = TRUE,
       note = 'включено ответом финблока §12 п.7 (27.08.2026): тактику публикуем под «ПРОДАЖИ»; BYN-пару заполняет ETL BI (§12 п.2); запись — под PLANS_PUBLISH_ENABLED',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL'
   AND block_type = 'sales_plan'
   AND param_name = 'ПРОДАЖИ'
   AND target_currency = '';
