UPDATE card_approval SET revoked = FALSE, revoked_reason = ''
 WHERE revoked_reason = 'шаг 1.2 заменён финансовым контролёром (маршрут 0066)';

UPDATE plans_form_route SET enabled = TRUE, sort_order = 35, responsible = 'финансист'
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = 'fin';
UPDATE plans_form_route SET enabled = TRUE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2';
UPDATE plans_form_route SET step_name = 'Директор направления', sort_order = 40
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.3';
UPDATE plans_form_route SET step_name = 'Утверждение', sort_order = 50
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.4';
UPDATE form_card SET step_code = '1.2', updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = 'fin'
   AND status IN ('on_approval', 'returned');
