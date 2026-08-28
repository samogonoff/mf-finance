-- Откат 0039 — возврат к состоянию до ответов финблока §12 (27.08.2026).
UPDATE plans_form_route
   SET enabled = FALSE,
       sort_order = 20,
       updated_at = NOW()
 WHERE step_code = 'fin'
   AND form_code IN ('TPL-MP', 'TPL-TO-RETAIL');

UPDATE publish_mapping
   SET enabled = FALSE,
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL'
   AND block_type = 'sales_plan'
   AND param_name = 'ПРОДАЖИ'
   AND target_currency = '';
